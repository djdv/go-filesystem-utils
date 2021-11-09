package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

var ErrCouldNotConnect = errors.New("could not connect to remote API")

//TODO: [current] we need a reliable way for callers to shutdown (and WAIT) for the launched process
// to exit. We can relay data to the caller via the request, maybe using Extra, or something.
// To start, it can just be magic (hardcoded string keys for req.Extra)
// req.extra[magic].WaitForSubProcExit()
//
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
		userMaddrs, err := daemon.UserServiceMaddrs()
		if err != nil {
			return nil, err
		}
		systemMaddrs, err := daemon.SystemServiceMaddrs()
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
		if !daemon.ServerDialable(serviceMaddr) {
			tried[i] = serviceMaddr.String()
			continue
		}
		if serviceMaddr == nil {
			fmt.Println("nil maddr in exec 103")
		}
		return daemon.GetClient(serviceMaddr)
	}

	if tryLaunching {
		autoExitInterval := settings.AutoExitInterval
		if autoExitInterval == 0 { // Don't linger around forever.
			autoExitInterval = 30 * time.Second
		}
		subCmd, serviceMaddr, err := relaunchSelfAsService(autoExitInterval)
		if err != nil {
			return nil, err
		}

		// XXX: `environment` will only be this type in our `_test` package.
		// This is not supported behavior and for validation only.
		// procPtr, callerWillManageProc := environment.(*procEnv)
		procPtr, callerWillManageProc := environment.(*exec.Cmd)
		if callerWillManageProc {
			// Caller must call proc.Wait() or proc.Release()
			*procPtr = *subCmd
			// Otherwise we call it now.
		} else if err := subCmd.Process.Release(); err != nil {
			return nil, err
		}

		if serviceMaddr == nil {
			fmt.Println("nil maddr in exec 131")
			fmt.Println("subCmd:", subCmd)
			fmt.Println("err:", err)
			// FIXME: sometimes this is nil,
			// causing a panic in GetClient
			// trace the source.
		}

		return daemon.GetClient(serviceMaddr)
	}

	return nil, fmt.Errorf("%w (tried: %s)",
		ErrCouldNotConnect, strings.Join(tried, ", "),
	)
}

type responseOrErr struct {
	*daemon.Response
	error
}

// relaunchSelfAsService creates a process,
// and waits for the service to return its address.
// Caller must call either cmd.Wait or cmd.proc.Release.
func relaunchSelfAsService(exitInterval time.Duration) (*exec.Cmd,
	multiaddr.Multiaddr, error) {
	// Initialize subprocess arguments and environment.
	self, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, err
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

	// Setup IPC
	//
	// NOTE: We don't send on this pipe.
	// It just exists for the subprocess to detect via its file mode bits.
	// (fs.ModeNamedPipe)
	if _, err := cmd.StdinPipe(); err != nil {
		return nil, nil, err
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
	}

	// Issue the command.
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	// Communicate with subprocess.
	const responseGrace = 10 * time.Second
	proc := cmd.Process
	serviceMaddrs, err := waitForServiceReady(stdoutPipe, stderrPipe, responseGrace)
	if err != nil {
		var (
			procState = cmd.ProcessState
			killProc  = procState != nil &&
				!procState.Exited()
		)
		if killProc {
			if procErr := proc.Kill(); procErr != nil {
				err = fmt.Errorf("%w - additionally could not kill subprocess (PID:%d): %s",
					err, proc.Pid, procErr)
			}
		}
		return nil, nil, fmt.Errorf("could not start background service: %w", err)
	}

	if len(serviceMaddrs) == 0 {
		return nil, nil, fmt.Errorf("service process returned no listeners")
	}
	serviceMaddr := serviceMaddrs[0]

	for _, maddr := range serviceMaddrs {
		if !daemon.ServerDialable(maddr) {
			return nil, nil,
				fmt.Errorf("service said it was ready but we could not connect (%s)",
					maddr.String(),
				)
		}
	}

	return cmd, serviceMaddr, nil
}

/* TODO lint
// waitForServiceReady scans the reader for signals from the service.
// Returning after the service is ready, encounters an error, or we time out.
func waitForServiceReady(stdout, stderr io.Reader, timeout time.Duration) (multiaddr.Multiaddr, error) {
	dbgLog, _ := os.OpenFile(`T:\exec.txt`, os.O_RDWR|os.O_TRUNC, 0)

	var (
		stdoutBuff bytes.Buffer
		stdoutTee  = io.TeeReader(stdout, &stdoutBuff)
		dbgOutTee  = io.TeeReader(stdoutTee, dbgLog)
		// serviceDecoder = json.NewDecoder(stdoutTee)
		serviceDecoder = json.NewDecoder(dbgOutTee)
		serviceErr     = make(chan error, 1)
		maddrChan      = make(chan multiaddr.Multiaddr, 1)
		timeoutChan    <-chan time.Time
	)

	go func() {
		for i := 0; ; i++ {
			var serviceResponse daemon.Response
			err := serviceDecoder.Decode(&serviceResponse)
			if errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				serviceErr <- err
				return
			}
			stdoutBuff.Reset()

			switch serviceResponse.Status {
			case daemon.Starting:
				encodedMaddr := serviceResponse.ListenerMaddr
				if encodedMaddr == nil {
					if i == 0 {
						continue // Expected to be nil only on the first response.
					}
					serviceErr <- errors.New("response did not contain listener maddr")
					return
				}
				maddrChan <- encodedMaddr.Interface
				continue

			case daemon.Status(0):
				if serviceResponse.Info == "" {
					// TODO: return an error
					// invalid message
				}
				continue
			case daemon.Ready:
				return
			}
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
	}()

	if timeout > 0 {
		timeoutChan = time.After(timeout)
	}
	select {
	case maddr := <-maddrChan:
		return maddr, nil
	case <-readyChan:
		return nil
	case <-timeoutChan:
		return nil, fmt.Errorf("timed out")
	case err := <-serviceErr:
		return nil, err
	}
}
*/

func waitForServiceReady(stdout, stderr io.Reader,
	timeout time.Duration) ([]multiaddr.Multiaddr, error) {
	var (
		ctx, cancel = context.WithTimeout(context.Background(), timeout)

		// stdout stream.
		stdResponses, respErrs = responsesFromStdio(ctx, stdout)
		responsePairs          = eitherRespOrErr(ctx, stdResponses, respErrs)

		// stderr stream.
		stdErrs = errorsFromStdio(ctx, stderr)

		// Merged channel.
		responses = splicePairsAndErrs(ctx, responsePairs, stdErrs)
	)
	defer cancel()

	if err := waitForStart(ctx, responses); err != nil {
		return nil, err
	}

	return aggregateListeners(ctx, responses)
}

func waitForStart(ctx context.Context, responses <-chan responseOrErr) error {
	for {
		select {
		case resp := <-responses:
			if resp.error != nil {
				return resp.error
			}
			if resp.Status == daemon.Starting {
				if resp.ListenerMaddr != nil {
					return errors.New("response contained listener maddr - should be empty")
				}
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func aggregateListeners(ctx context.Context,
	responses <-chan responseOrErr) ([]multiaddr.Multiaddr, error) {
	var maddrs []multiaddr.Multiaddr
	for {
		select {
		case resp := <-responses:
			switch {
			case resp.error != nil:
				return nil, resp.error
			case resp.Status == daemon.Starting:
				listener := resp.ListenerMaddr
				if listener == nil {
					return nil,
						errors.New("response contained nil listener maddr - should be populated")
				}
				maddrs = append(maddrs, listener)
			case resp.Status == daemon.Ready:
				return maddrs, nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func eitherRespOrErr(ctx context.Context,
	responses <-chan *daemon.Response, errs <-chan error) <-chan responseOrErr {
	pairChan := make(chan responseOrErr)
	go func() {
		defer close(pairChan)
		for responses != nil ||
			errs != nil {
			var (
				pair responseOrErr
				ok   bool
			)
			select {
			case pair.Response, ok = <-responses:
				if !ok {
					responses = nil
					continue
				}
			case pair.error, ok = <-errs:
				if !ok {
					errs = nil
					continue
				}
			case <-ctx.Done():
				return
			}
			select {
			case pairChan <- pair:
			case <-ctx.Done():
				return
			}
		}
	}()
	return pairChan
}

func splicePairsAndErrs(ctx context.Context,
	pairs <-chan responseOrErr, errs <-chan error) <-chan responseOrErr {
	pairChan := make(chan responseOrErr)
	go func() {
		defer close(pairChan)
		for pairs != nil ||
			errs != nil {
			var (
				pair responseOrErr
				err  error
				ok   bool
			)
			// Either relay a pair we receive
			// or insert an error into the pair channel.
			select {
			case pair, ok = <-pairs:
				if !ok {
					pairs = nil
					continue
				}
			case err, ok = <-errs:
				if !ok {
					errs = nil
					continue
				}
				pair.error = err
			case <-ctx.Done():
				return
			}

			select {
			case pairChan <- pair:
			case <-ctx.Done():
				return
			}
		}
	}()
	return pairChan
}

func responsesFromStdio(ctx context.Context,
	stdout io.Reader) (<-chan *daemon.Response, <-chan error) {
	var (
		stdResponses = make(chan *daemon.Response)
		responseErrs = make(chan error)
	)
	go func() {
		defer close(responseErrs)
		defer close(stdResponses)
		var (
			stdoutBuff     bytes.Buffer
			stdoutTee      = io.TeeReader(stdout, &stdoutBuff)
			serviceDecoder = json.NewDecoder(stdoutTee)
			firstResponse  = true
		)
		for {
			serviceResponse := new(daemon.Response)
			err := serviceDecoder.Decode(serviceResponse)
			if errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				responseErrs <- err
				return
			}
			stdoutBuff.Reset()

			if err := validateResponse(firstResponse, serviceResponse); err != nil {
				select {
				case responseErrs <- err:
					continue
				case <-ctx.Done():
					return
				}
			}

			firstResponse = false
			select {
			case stdResponses <- serviceResponse:
			case <-ctx.Done():
				return
			}
			if serviceResponse.Status == daemon.Ready {
				return
			}
		}
		select {
		case responseErrs <- fmt.Errorf(
			"process output ended abruptly - last response: %s",
			stdoutBuff.String(),
		):
		case <-ctx.Done():
		}
	}()

	return stdResponses, responseErrs
}

func validateResponse(firstResponse bool, response *daemon.Response) (err error) {
	switch response.Status {
	case daemon.Starting:
		encodedMaddr := response.ListenerMaddr
		if !firstResponse &&
			encodedMaddr == nil {
			// Expected to be nil only on the first response.
			err = errors.New("response did not contain listener maddr")
		}
	case daemon.Status(0):
		if response.Info == "" {
			err = errors.New("malformed message / empty values")
		}
	case daemon.Ready:
	default:
		err = fmt.Errorf("unexpected message: %#v", response)
	}
	return
}

func errorsFromStdio(ctx context.Context, stderr io.Reader) <-chan error {
	stdErrs := make(chan error)
	go func() {
		defer close(stdErrs)
		errScanner := bufio.NewScanner(stderr)
		for {
			if !errScanner.Scan() {
				return
			}
			text := errScanner.Text()
			select {
			case stdErrs <- fmt.Errorf("got error from stderr: %s", text):
			case <-ctx.Done():
				return
			}
		}
	}()
	return stdErrs
}
