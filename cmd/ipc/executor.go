package ipc

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
		userMaddrs, err := fscmds.UserServiceMaddrs()
		if err != nil {
			return nil, err
		}
		systemMaddrs, err := fscmds.SystemServiceMaddrs()
		if err != nil {
			return nil, err
		}

		// If none of these are available,
		// we'll try to launch an instance ourselves.
		serviceMaddrs = append(userMaddrs, systemMaddrs...)
		tryLaunching = true
	}

	var (
		clientHost  string
		clientOpts  []cmdshttp.ClientOpt
		foundServer bool
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
		autoExitInterval := settings.AutoExitInterval
		if autoExitInterval == 0 { // Don't linger around forever.
			autoExitInterval = 30 * time.Second
		}
		pid, serviceMaddr, err := relaunchSelfAsService(autoExitInterval)
		if err != nil {
			return nil, err
		}
		clientHost, clientOpts, err = parseCmdsClientOptions(serviceMaddr)
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

	cmd := exec.Command(self, ServiceCommandName)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	if exitInterval != 0 {
		cmd.Args = append(cmd.Args,
			fmt.Sprintf("--%s=%s", fscmds.AutoExitInterval().CommandLine(), exitInterval),
		)
	}

	// Setup IPC
	servicePipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
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
	serviceMaddr, err := waitForService(servicePipe, startGrace)
	if err != nil {
		if procState != nil &&
			!procState.Exited() {
			// Subprocess is still running after a fault.
			// Implementation fault in service command is implied.
			if procErr := proc.Kill(); procErr != nil {
				err = fmt.Errorf("%w - additionally could not kill subprocess (PID:%d): %s",
					err, proc.Pid, procErr)
			}
		}
		return nil, nil, fmt.Errorf("could not start background service: %w", err)
	}

	if !fscmds.ServerDialable(serviceMaddr) {
		return nil, nil, fmt.Errorf("service said it was ready but we could not connect")
	}

	// Process passed our checks,
	// release it and proceed ourselves.
	releasedPid := proc.Pid
	if err := proc.Release(); err != nil {
		return nil, nil, err
	}

	return &releasedPid, serviceMaddr, servicePipe.Close()
}

// waitForService scans the reader for signals from the service.
// Returning after the service is ready, encounters an error, or we time out.
func waitForService(input io.Reader, timeout time.Duration) (multiaddr.Multiaddr, error) {
	var (
		serviceScanner = bufio.NewScanner(input)
		serviceErr     = make(chan error, 1)
		maddrChan      = make(chan multiaddr.Multiaddr, 1)
		timeoutChan    <-chan time.Time
	)

	go func() { // STDIO handling.
		serviceScanner.Scan()
		{
			text := serviceScanner.Text()
			if !strings.Contains(text, StdHeader) {
				serviceErr <- fmt.Errorf("unexpected process output:"+
					"\n\twanted: %s"+
					"\n\tgot: %s",
					StdHeader, text,
				)
				return
			}
		}

		var text string
		for serviceScanner.Scan() {
			text = serviceScanner.Text()
			maddrIndex := strings.Index(text, StdGoodStatus)
			if maddrIndex >= 0 {
				maddrIndex += len(StdGoodStatus)
				maddrString := text[maddrIndex:]
				if maddr, err := multiaddr.NewMultiaddr(maddrString); err == nil {
					maddrChan <- maddr
				} else {
					serviceErr <- fmt.Errorf("could not parse maddr received from server: %w", err)
				}
				return
			}
			if strings.Contains(text, StdReady) {
				serviceErr <- fmt.Errorf("service reported ready with no listeners")
				return
			}
		}
		serviceErr <- fmt.Errorf("process output ended abruptly: %s", text)
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
