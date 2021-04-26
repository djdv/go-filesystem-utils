package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	cmds "github.com/ipfs/go-ipfs-cmds"
	cmdshttp "github.com/ipfs/go-ipfs-cmds/http"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

func MakeExecutor(request *cmds.Request, environment interface{}) (cmds.Executor, error) {
	// Execute the request locally if we can.
	if request.Command.NoRemote ||
		!request.Command.NoLocal {
		return cmds.NewExecutor(request.Root), nil
	}

	// Everything else connects as a client.
	var tryLaunching bool
	serviceMaddr, addrWasProvided, err := fscmds.GetServiceMaddr(request)
	if err != nil {
		if !errors.Is(err, fscmds.ErrServiceNotFound) {
			return nil, err
		}
		// When no arguments are provided, try auto connecting to
		// and/or launching a service instance.
		tryLaunching = true
	}

	if !addrWasProvided &&
		tryLaunching {
		pid, err := relaunchSelfAsService(request)
		if err != nil {
			return nil, err
		}
		// XXX: Don't look at this, it's only used in our _test package.
		if pidPtr, ok := environment.(*int); ok {
			*pidPtr = *pid
		}
		serviceMaddr, _, err = fscmds.GetServiceMaddr(request)
		if err != nil {
			return nil, err
		}
	}

	clientHost, clientOpts, err := parseCmdsClientOptions(serviceMaddr)
	if err != nil {
		return nil, err
	}

	return cmdshttp.NewClient(clientHost, clientOpts...), nil
}

func parseCmdsClientOptions(maddr multiaddr.Multiaddr) (clientHost string, clientOpts []cmdshttp.ClientOpt, err error) {
	network, dialHost, err := manet.DialArgs(maddr)
	if err != nil {
		return "", nil, err
	}
	switch network {
	case "tcp", "tcp4", "tcp6":
		clientHost = dialHost
	case "unix":
		// TODO: Consider patching cmds-lib.
		// We want to use the URL scheme "http+unix".
		// As-is, it prefixes the value to be parsed by pkg `url` as "http://http+unix://".
		clientHost = fmt.Sprintf("http://%s-%s", fscmds.ServiceName, fscmds.ServerName)
		netDialer := new(net.Dialer)
		clientOpts = append(clientOpts, cmdshttp.ClientWithHTTPClient(&http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return netDialer.DialContext(ctx, network, dialHost)
				},
			},
		}))
	default:
		return "", nil, fmt.Errorf("unsupported API address: %s", maddr)
	}
	return clientHost, clientOpts, nil
}

func relaunchSelfAsService(request *cmds.Request) (*int, error) {
	// Initialize subprocess arguments and environment.
	// Surprise, we're argv[0].
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	// AutoExit value from request or a default.
	serviceStopAfter, provided, err := fscmds.GetDurationArgument(request, fscmds.AutoExitParameter)
	if err != nil {
		return nil, err
	}
	if !provided {
		serviceStopAfter = stopGrace
	}

	cmd := exec.Command(self, Name,
		fmt.Sprintf("--%s=%s", fscmds.AutoExitParameter.Name, serviceStopAfter),
	)
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	// Setup IPC
	servicePipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	// Issue the command.
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	proc := cmd.Process
	procState := cmd.ProcessState

	// Communicate with subprocess.
	err = waitForService(servicePipe, startGrace)
	if err != nil {
		if !procState.Exited() {
			// Subprocess is still running after a fault.
			// Implementation fault in service command is implied.
			kErr := proc.Kill() // Vae puer meus victis est.
			if kErr != nil {
				err = fmt.Errorf("%w - additionally could not kill subprocess (PID:%d): %s",
					err, proc.Pid, kErr)
			}
		}
		return nil, fmt.Errorf("could not start background service: %w", err)
	}

	// Process passed our checks,
	// release it and proceed ourselves.
	releasedPid := proc.Pid
	if err = proc.Release(); err != nil {
		return nil, err
	}

	return &releasedPid, servicePipe.Close()
}

// waitForService scans the reader for signals from the service.
// Returning after the service is ready, encounters an error, or we time out.
func waitForService(input io.Reader, timeout time.Duration) error {
	var (
		serviceScanner = bufio.NewScanner(input)
		scannerErr     = make(chan error, 1)
		timeoutChan    <-chan time.Time
	)
	go func() {
		defer close(scannerErr)
		serviceScanner.Scan()
		{
			text := serviceScanner.Text()
			if !strings.Contains(text, stdHeader) {
				scannerErr <- fmt.Errorf("unexpected process output: %s", text)
				return
			}
		}

		var text string
		for serviceScanner.Scan() {
			text = serviceScanner.Text()
			if strings.Contains(text, stdReady) {
				return
			}
		}
		scannerErr <- fmt.Errorf("process output ended abruptly: %s", text)
	}()

	if timeout > 0 {
		timeoutChan = time.After(timeout)
	}
	select {
	case <-timeoutChan:
		return fmt.Errorf("timed out")
	case err := <-scannerErr:
		return err
	}
}
