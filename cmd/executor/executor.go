package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	fscmds "github.com/djdv/go-filesystem-utils/filesystem/cmds"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

var ErrCouldNotConnect = errors.New("could not connect to remote API")

// TODO check if comment is out of date
// MakeExecutor constructs a cmds-lib executor; which parses the Request and
// determines whether to execute the Command within the same local process,
// or within a remote service instance's process.
//
// If no remote addresses are provided in the request,
// and no default instances respond to our checks -
// a local service instance will be created automatically,
// and used to satisfy the request.
func MakeExecutor(request *cmds.Request, env interface{}) (cmds.Executor, error) {
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
		cmd              = request.Command
		serviceMaddrs    = settings.ServiceMaddrs
		autoExitInterval = settings.AutoExitInterval
	)

	return connectToOrLaunchDaemon(cmd, env, autoExitInterval, serviceMaddrs...)
}

func fLaunch(env cmds.Environment, autoExitInterval time.Duration) (cmds.Executor, error) {
	if autoExitInterval == 0 { // Don't linger around forever.
		autoExitInterval = 30 * time.Second
	}
	subCmd, serviceMaddr, err := relaunchSelfAsService(autoExitInterval)
	if err != nil {
		return nil, err
	}
	// XXX: `environment` will only be this type in our `_test` package.
	// This is not supported behavior and for validation only.
	cmdPtr, callerWillManageProc := env.(*exec.Cmd)
	if callerWillManageProc {
		// Caller must call proc.Wait() or proc.Release()
		*cmdPtr = *subCmd
		// Otherwise call it now.
	} else if err := subCmd.Process.Release(); err != nil {
		return nil, err
	}

	return daemon.GetClient(serviceMaddr)
}

func connectToOrLaunchDaemon(cmd *cmds.Command, env cmds.Environment,
	idleCheck time.Duration, args ...multiaddr.Multiaddr) (cmds.Executor, error) {
	if len(args) > 0 {
		return getFirstConnection(args...)
	}

	var defaults []multiaddr.Multiaddr
	for _, fn := range []func() ([]multiaddr.Multiaddr, error){
		daemon.UserServiceMaddrs,
		daemon.SystemServiceMaddrs,
	} {
		maddrs, err := fn()
		if err != nil {
			return nil, err
		}
		defaults = append(defaults, maddrs...)
	}
	client, err := connectToOrLaunchDaemon(cmd, env, idleCheck, defaults...)
	if err == nil && client != nil {
		return client, nil
	}
	if !errors.Is(err, ErrCouldNotConnect) {
		return nil, err
	}
	// Don't launch daemon, just to stop it.
	if cmd == stop.Command {
		return nil, ErrCouldNotConnect
	}

	// Launch daemon, and try again via defaults.
	return fLaunch(env, idleCheck)
}

func getFirstConnection(args ...multiaddr.Multiaddr) (cmds.Executor, error) {
	var errs []error
	for result := range testConnection(generateMaddrs(args...)) {
		if err := result.error; err != nil {
			errs = append(errs, err)
			continue
		}
		client, err := daemon.GetClient(result.Multiaddr)
		if err == nil {
			return client, nil
		}
		errs = append(errs, err)
	}
	err := ErrCouldNotConnect
	for _, e := range errs {
		err = fmt.Errorf("%w\n\t%s", err, e)
	}
	return nil, err
}

type maddrResult struct {
	multiaddr.Multiaddr
	error
}

func generateMaddrs(maddrs ...multiaddr.Multiaddr) <-chan multiaddr.Multiaddr {
	maddrChan := make(chan multiaddr.Multiaddr, len(maddrs))
	go func() {
		defer close(maddrChan)
		for _, maddr := range maddrs {
			maddrChan <- maddr
		}
	}()
	return maddrChan
}

func testConnection(maddrs <-chan multiaddr.Multiaddr) <-chan maddrResult {
	results := make(chan maddrResult, cap(maddrs))
	go func() {
		defer close(results)
		for maddr := range maddrs {
			var result maddrResult
			if daemon.ServerDialable(maddr) {
				result.Multiaddr = maddr
			} else {
				result.error = fmt.Errorf(
					"could not connect to %s",
					maddr.String(),
				)
			}
			results <- result
		}
	}()
	return results
}

// relaunchSelfAsService creates a process,
// and waits for the service to return its address.
// Caller must call either cmd.Wait or cmd.proc.Release.
func relaunchSelfAsService(exitInterval time.Duration) (*exec.Cmd, multiaddr.Multiaddr, error) {
	cmd, err := selfCommand(exitInterval)
	if err != nil {
		return nil, nil, err
	}
	sio, err := setupIPC(cmd)
	if err != nil {
		return nil, nil, err
	}

	serviceMaddr, err := startAndCommunicateWith(cmd, sio)
	if err != nil {
		return nil, nil, err
	}

	if err := detatchIO(sio); err != nil {
		var (
			proc      = cmd.Process
			procState = cmd.ProcessState
			killProc  = procState != nil &&
				!procState.Exited()
		)
		if killProc {
			pid := proc.Pid
			if procErr := proc.Kill(); procErr != nil {
				err = fmt.Errorf("%w"+
					"\n\tcould not kill subprocess (PID:%d): %s",
					err, pid, procErr,
				)
			}
		}
		return nil, nil, fmt.Errorf("could not start background service: %w", err)
	}

	return cmd, serviceMaddr, nil
}

func selfCommand(exitInterval time.Duration) (*exec.Cmd, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(self, daemon.CmdsPath()...)
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
	return cmd, nil
}

type stdio struct {
	stdin          io.WriteCloser
	stdout, stderr io.ReadCloser
}

func setupIPC(cmd *exec.Cmd) (sio stdio, err error) {
	if sio.stdin, err = cmd.StdinPipe(); err != nil {
		return
	}
	if sio.stdout, err = cmd.StdoutPipe(); err != nil {
		return
	}
	if sio.stderr, err = cmd.StderrPipe(); err != nil {
		return
	}
	return
}

func startAndCommunicateWith(cmd *exec.Cmd, sio stdio) (multiaddr.Multiaddr, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	const responseGrace = 10 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), responseGrace)
	defer cancel()

	responses := daemon.ResponsesFromReaders(ctx, sio.stdout, sio.stderr)
	serviceMaddr, err := getFirstServer(responses)
	if err != nil {
		return nil, err
	}

	return serviceMaddr, nil
}

func getFirstServer(results <-chan daemon.ResponseResult) (multiaddr.Multiaddr, error) {
	if err := daemon.UntilStarting(results, nil); err != nil {
		return nil, err
	}
	var (
		serverMaddr      multiaddr.Multiaddr
		responseCallback = func(response *daemon.Response) error {
			if haveWorkingServer := serverMaddr != nil; haveWorkingServer {
				return nil
			}
			if maddr := response.ListenerMaddr; maddr != nil {
				if !daemon.ServerDialable(maddr) {
					return fmt.Errorf(
						"service said it was ready but we could not connect (%s)",
						maddr.String(),
					)
				}
				serverMaddr = maddr
			}
			return nil
		}
	)
	if err := daemon.UntilReady(results, responseCallback); err != nil {
		return nil, err
	}
	if serverMaddr == nil {
		return nil, fmt.Errorf("daemon process did not return any server addresses")
	}

	return serverMaddr, nil
}

func detatchIO(sio stdio) error {
	if err := sendDoneSignalToRemote(sio.stdin); err != nil {
		return err
	}
	for _, closer := range []io.Closer{sio.stdin, sio.stdout, sio.stderr} {
		if err := closer.Close(); err != nil {
			return err
		}
	}
	return nil
}

func sendDoneSignalToRemote(stdinPipe io.Writer) error {
	asciiEOT := [...]byte{daemon.ASCIIEOT}
	bytesWrote, err := stdinPipe.Write(asciiEOT[:])
	if err != nil {
		return err
	}
	if signalLen := len(asciiEOT); signalLen != bytesWrote {
		return fmt.Errorf(
			"bytes written %d != len(EOT bytes)(%d): %#v",
			bytesWrote, signalLen, asciiEOT)
	}
	return nil
}
