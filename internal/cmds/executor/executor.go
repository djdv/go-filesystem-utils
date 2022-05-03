package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/djdv/go-filesystem-utils/cmd/service"
	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

var ErrCouldNotConnect = errors.New("could not connect to remote API")

// MakeExecutor constructs a cmds-lib executor; which parses the Request and
// determines whether to execute the Command within a local or remote process.
//
// If no remote addresses are provided in the request,
// a local service instance will be created and used automatically.
func MakeExecutor(request *cmds.Request, _ interface{}) (cmds.Executor, error) {
	// Execute the request locally if we can.
	if request.Command.NoRemote ||
		!request.Command.NoLocal {
		return cmds.NewExecutor(request.Root), nil
	}

	// Everything else connects as a client.
	ctx := request.Context
	settings, err := settings.Parse[*settings.Root](ctx, request)
	if err != nil {
		return nil, err
	}
	var (
		serviceMaddrs    = settings.ServiceMaddrs
		autoExitInterval = settings.AutoExitInterval
	)
	if len(serviceMaddrs) > 0 {
		activeMaddr, err := getFirstDialable(ctx, serviceMaddrs...)
		if err != nil {
			return nil, err
		}
		return daemon.MakeClient(activeMaddr)
	}

	defaultMaddrs, err := defaultMaddrs()
	if err != nil {
		return nil, err
	}
	switch activeMaddr, err := getFirstDialable(ctx, defaultMaddrs...); {
	case err == nil:
		return daemon.MakeClient(activeMaddr)
	case errors.Is(err, ErrCouldNotConnect):
		// TODO: don't spawn server if request.Command is `stop` command.
		// We can no longer compare pointer values, so we need a new way to check
		// equality of commands.
		// Maybe something like command.Extra.DontStart would be general and good.
		return launchServerAndConnect(autoExitInterval)
	default:
		return nil, err
	}
}

func getFirstDialable(ctx context.Context,
	maddrs ...multiaddr.Multiaddr,
) (multiaddr.Multiaddr, error) {
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	dialable := dialableMaddrs(subCtx, generate(subCtx, maddrs...))
	select {
	case dialableMaddr, ok := <-dialable:
		if !ok {
			return nil, ErrCouldNotConnect // TODO: add context to message. Canceled? tried which?
		}
		return dialableMaddr, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TODO: use handshake instead when implemented in server code.
// ^ maybe we should return the net.Conn instead of a maddr
// let the caller use and/or close it. This will be necessary for the handshake.
// ^^ it'd make more sense to just do maddrs=>cmdsClients->handshake()=>cmdsClients
// where the cmdslib will call our (ctx)dial itself internally.
// TODO: review - make sure we exit cleanly when not needed
func dialableMaddrs(ctx context.Context, maddrs <-chan multiaddr.Multiaddr) <-chan multiaddr.Multiaddr {
	var (
		maddrCount = len(maddrs)
		dialable   = make(chan multiaddr.Multiaddr, maddrCount)
	)
	go func() {
		defer close(dialable)
		var (
			dialerWg sync.WaitGroup
			dial     = func(maddr multiaddr.Multiaddr) error {
				dialerWg.Add(1)
				go func() {
					defer dialerWg.Done()
					if daemon.ServerDialable(maddr) {
						dialable <- maddr // no select (buffered).
					}
				}()
				return nil
			}
		)
		generic.ForEachOrError(ctx, maddrs, nil, dial)
		dialerWg.Wait()
	}()
	return dialable
}

func defaultMaddrs() ([]multiaddr.Multiaddr, error) {
	var (
		sources = []func() ([]multiaddr.Multiaddr, error){
			daemon.UserServiceMaddrs,
			daemon.SystemServiceMaddrs,
		}
		defaults []multiaddr.Multiaddr
	)
	for _, defaultMaddrs := range sources {
		maddrs, err := defaultMaddrs()
		if err != nil {
			return nil, err
		}
		defaults = append(defaults, maddrs...)
	}
	return defaults, nil
}

// TODO: better names
func launchServerAndConnect(autoExitInterval time.Duration) (cmds.Executor, error) {
	if autoExitInterval == 0 { // Don't linger around forever.
		autoExitInterval = 30 * time.Second
	}
	serviceMaddr, err := startService(autoExitInterval)
	if err != nil {
		return nil, err
	}
	return daemon.MakeClient(serviceMaddr)
}

func generate[typ any](ctx context.Context, inputs ...typ) <-chan typ {
	out := make(chan typ, len(inputs))
	go func() {
		defer close(out)
		for _, in := range inputs {
			if ctx.Err() != nil { // Non-select check / full-buffered receiver.
				return
			}
			out <- in
		}
	}()
	return out
}

// startService creates a service process and
// returns the addresses received from that process.
func startService(exitInterval time.Duration) (multiaddr.Multiaddr, error) {
	cmd, err := newServiceCommand(exitInterval)
	if err != nil {
		return nil, err
	}
	sio, err := setupIPC(cmd)
	if err != nil {
		return nil, err
	}

	serviceMaddr, err := startAndCommunicateWith(cmd, sio)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("could not start background service: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return nil, err
	}

	return serviceMaddr, nil
}

func newServiceCommand(exitInterval time.Duration) (*exec.Cmd, error) {
	execName, err := os.Executable()
	if err != nil {
		return nil, err
	}

	// TODO: [maybe] walk cmds.root until subcommand == daemon.Name?
	execArgs := []string{
		service.Name, daemon.Name,
		fmt.Sprintf("--%s=%s",
			cmds.EncShort, cmds.JSON,
		),
	}
	if exitInterval != 0 {
		execArgs = append(execArgs,
			fmt.Sprintf("--%s=%s",
				settings.AutoExitParam().Name(parameters.CommandLine), exitInterval,
			),
		)
	}
	cmd := exec.Command(execName, execArgs...)
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
