package executor

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

var ErrCouldNotConnect = errors.New("could not connect to remote API")

// MakeExecutor constructs a cmds-lib executor; which parses the Request and
// determines whether to execute the Command within the same local process,
// or within a remote service instance's process.
//
// If no remote addresses are provided in the request,
// and no default instances respond to our checks -
// a local service instance will be created automatically,
// and used to satisfy the request.
func MakeExecutor(request *cmds.Request, environment interface{}) (cmds.Executor, error) {
	// Execute the request locally if we can.
	if request.Command.NoRemote ||
		!request.Command.NoLocal {
		return cmds.NewExecutor(request.Root), nil
	}

	// Everything else connects as a client.
	var (
		ctx             = request.Context
		settings        = new(fscmds.Settings)
		unsetArgs, errs = parameters.ParseSettings(ctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
		_, err = parameters.AccumulateArgs(ctx, unsetArgs, errs)
	)
	if err != nil {
		return nil, err
	}

	var (
		serviceMaddrs = settings.ServiceMaddrs
		tryLaunching  bool
	)
	if len(serviceMaddrs) == 0 {
		// When service maddrs aren't provided,
		// provide+check a default set.
		userMaddrs, err := ipc.UserServiceMaddrs()
		if err != nil {
			return nil, err
		}
		systemMaddrs, err := ipc.SystemServiceMaddrs()
		if err != nil {
			return nil, err
		}

		// If none of these are available,
		// we'll try to launch an instance ourselves.
		serviceMaddrs = append(userMaddrs, systemMaddrs...)
		tryLaunching = true
	}

	// TODO: we need a special handler for this
	// `stop` should dial all `--api` and broadcast to all of them
	// via some kind of Executor plexer.
	// Alternatively stop could take a vector of maddrs, but this seems bad.
	// (because then the remote is requesting stop on another remote, rather than local main)
	//
	// TODO: change this condition. Break the whole thing out into a separate function.
	// We should call `stop` locally if we can't dial.
	// (So that it may return its own "not started" error - rather than us returning one)
	if request.Command == stop.Command {
		// Don't spawn an instance just to stop it.
		tryLaunching = false
	}

	tried := make([]string, len(serviceMaddrs))
	for i, serviceMaddr := range serviceMaddrs {
		if !ipc.ServerDialable(serviceMaddr) {
			tried[i] = serviceMaddr.String()
			continue
		}
		return ipc.GetClient(serviceMaddr)
	}

	if tryLaunching {
		autoExitInterval := settings.AutoExitInterval
		if autoExitInterval == 0 { // Don't linger around forever.
			autoExitInterval = 30 * time.Second
		}
		pid, serviceMaddr, err := relaunchSelfAsService(autoExitInterval)
		if err != nil {
			return nil, err
		}
		// XXX: Don't look at this, and don't rely on it.
		// `environment` will only be an int pointer in our `_test` package.
		// This is not supported behaviour and for validation only.
		if pidPtr, ok := environment.(*int); ok {
			*pidPtr = *pid
		}
		return ipc.GetClient(serviceMaddr)
	}

	return nil, fmt.Errorf("%w (tried: %s)",
		ErrCouldNotConnect, strings.Join(tried, ", "),
	)
}

func relaunchSelfAsService(exitInterval time.Duration) (*int, multiaddr.Multiaddr, error) {
	// Initialize subprocess arguments and environment.
	self, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}

	cmd := exec.Command(self, fscmds.DaemonCmdsPath()...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	if exitInterval != 0 {
		cmd.Args = append(cmd.Args,
			fmt.Sprintf("--%s=%s",
				fscmds.AutoExitInterval().CommandLine(), exitInterval,
			),
			fmt.Sprintf("--%s=%s",
				cmds.EncShort, cmds.JSON,
			),
		)
	}

	// Setup IPC
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
	}

	closePipes := func() error {
		var (
			err       error
			stdoutErr = stdoutPipe.Close()
			stderrErr = stderrPipe.Close()
		)
		if stdoutErr != nil {
			err = fmt.Errorf("could not close stdout: %w", stdoutErr)
		}
		if stderrErr != nil {
			stderrErr = fmt.Errorf("could not close stderr: %w", stderrErr)
			if err == nil {
				err = stderrErr
			} else {
				err = fmt.Errorf("%w - %s", err, stderrErr)
			}
		}
		return err
	}

	// Issue the command.
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	// Communicate with subprocess.
	const startGrace = 10 * time.Second
	var (
		proc      = cmd.Process
		procState = cmd.ProcessState
	)
	serviceMaddr, err := waitForService(stdoutPipe, stderrPipe, startGrace)
	if err != nil {
		if procState != nil &&
			!procState.Exited() { // XXX: The error formatting here could be better.
			if pipeErrs := closePipes(); pipeErrs != nil {
				err = fmt.Errorf("%w - additionally could not close subprocess pipes: %s",
					err, pipeErrs)
			}
			// Subprocess is still running after a fault.
			// Implementation fault in service command is implied.
			if procErr := proc.Kill(); procErr != nil {
				err = fmt.Errorf("%w - additionally could not kill subprocess (PID:%d): %s",
					err, proc.Pid, procErr)
			}
		}
		return nil, nil, fmt.Errorf("could not start background service: %w", err)
	}

	if !ipc.ServerDialable(serviceMaddr) {
		return nil, nil, fmt.Errorf("service said it was ready but we could not connect")
	}

	// Process passed our checks,
	// release it and proceed ourselves.
	releasedPid := proc.Pid
	if err := proc.Release(); err != nil {
		return nil, nil, err
	}

	return &releasedPid, serviceMaddr, closePipes()
}

// waitForService scans the reader for signals from the service.
// Returning after the service is ready, encounters an error, or we time out.
func waitForService(stdout, stderr io.Reader, timeout time.Duration) (multiaddr.Multiaddr, error) {
	var (
		stdoutBuff     bytes.Buffer
		stdoutTee      = io.TeeReader(stdout, &stdoutBuff)
		serviceDecoder = json.NewDecoder(stdoutTee)
		serviceErr     = make(chan error, 1)
		maddrChan      = make(chan multiaddr.Multiaddr, 1)
		timeoutChan    <-chan time.Time
	)

	go func() {
		for i := 0; ; i++ {
			var serviceResponse ipc.ServiceResponse
			if err := serviceDecoder.Decode(&serviceResponse); err == io.EOF {
				break
			} else if err != nil {
				serviceErr <- err
				return
			}
			stdoutBuff.Reset()

			if i == 0 {
				if serviceResponse.Status != ipc.ServiceStarting {
					expectedJson, err := json.Marshal(&ipc.ServiceResponse{
						Status: ipc.ServiceStarting,
					})
					if err != nil {
						serviceErr <- fmt.Errorf("implementation error: %w", err)
						return
					}
					serviceErr <- fmt.Errorf("unexpected process output"+
						"\n\tExpected: %s"+
						"\n\tGot: %s",
						expectedJson,
						stdoutBuff.String(),
					)
					return
				}
				continue
			}

			if serviceResponse.Status != ipc.ServiceReady {
				continue
			}

			encodedMaddr := serviceResponse.ListenerMaddr
			if encodedMaddr == nil {
				serviceErr <- fmt.Errorf("service reported ready with no listeners")
				return
			}
			maddrChan <- encodedMaddr.Interface
			return
		}
		serviceErr <- fmt.Errorf("process output ended abruptly - last response: %s",
			stdoutBuff.String(),
		)
	}()
	go func() {
		errScanner := bufio.NewScanner(stderr)
		if !errScanner.Scan() {
			return
		}
		text := errScanner.Text()
		serviceErr <- fmt.Errorf("got error from stderr: %s", text)
		return
	}()

	if timeout > 0 {
		timeoutChan = time.After(timeout)
	}
	select {
	case maddr := <-maddrChan:
		return maddr, nil
	case <-timeoutChan:
		return nil, fmt.Errorf("timed out")
	case err := <-serviceErr:
		return nil, err
	}
}
