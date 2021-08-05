package service

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
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
		serviceMaddrs             = settings.ServiceMaddrs
		foundServer, tryLaunching bool
	)
	if len(serviceMaddrs) == 0 {
		// When service maddrs aren't provided,
		// check a default set.
		userMaddrs, err := fscmds.UserServiceMaddrs()
		if err != nil {
			return nil, err
		}
		systemMaddrs, err := fscmds.SystemServiceMaddrs()
		if err != nil {
			return nil, err
		}
		serviceMaddrs = append(userMaddrs, systemMaddrs...)

		// If none of these are available,
		// we'll try to launch and instance ourselves.
		tryLaunching = true
		if settings.AutoExitInterval == 0 { // Don't linger around forever.
			settings.AutoExitInterval = 30 * time.Second
		}
	}

	var (
		clientHost string
		clientOpts []cmdshttp.ClientOpt
	)
	for _, serviceMaddr := range serviceMaddrs {
		if !fscmds.ServerDialable(serviceMaddr) {
			continue
		}
		clientHost, clientOpts, err = parseCmdsClientOptions(serviceMaddr)
		if err != nil {
			return nil, err
		}
		foundServer = true
		break
	}

	if !foundServer && tryLaunching {
		// The first in this list should be most specific to the user.
		// And thus most likely to succeed.
		// We don't want to try all of them.
		localMaddr := serviceMaddrs[0]
		pid, err := relaunchSelfAsService(settings.AutoExitInterval, localMaddr)
		if err != nil {
			return nil, err
		}
		clientHost, clientOpts, err = parseCmdsClientOptions(localMaddr)
		if err != nil {
			return nil, err
		}
		// XXX: Don't look at this, and don't rely on it.
		// `environment` will only be an int pointer in our `_test` package.
		// This is not supported behaviour and for validation only.
		if pidPtr, ok := environment.(*int); ok {
			*pidPtr = *pid
		}
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

func relaunchSelfAsService(exitAfter time.Duration,
	serviceMaddrs ...multiaddr.Multiaddr) (*int, error) {
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

	cmd := exec.Command(self, Name)
	if exitAfter != 0 {
		cmd.Args = append(cmd.Args,
			fmt.Sprintf("--%s=%s", fscmds.AutoExitInterval().CommandLine(), exitAfter),
		)
	}
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
		if procState != nil &&
			!procState.Exited() {
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
