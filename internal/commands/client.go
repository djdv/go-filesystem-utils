package commands

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adrg/xdg"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/p9/p9"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/u-root/uio/ulog"
)

type (
	Client         p9.Client
	clientSettings struct {
		serviceMaddr multiaddr.Multiaddr
		log          ulog.Logger
		exitInterval time.Duration
	}
	clientOption  func(*clientSettings) error
	clientOptions []clientOption
)

const (
	exitIntervalDefault  = 30 * time.Second
	errServiceConnection = generic.ConstError("could not connect to service")
	errCouldNotDial      = generic.ConstError("could not dial")
)

func (cs *clientSettings) getClient(autoLaunchDaemon bool) (*Client, error) {
	var (
		serviceMaddr = cs.serviceMaddr
		options      []p9.ClientOpt
	)
	if log := cs.log; log != nil {
		options = append(options, p9.WithClientLogger(log))
	}
	var serviceMaddrs []multiaddr.Multiaddr
	if serviceMaddr != nil {
		autoLaunchDaemon = false
		serviceMaddrs = []multiaddr.Multiaddr{serviceMaddr}
	} else {
		var err error
		if serviceMaddrs, err = allServiceMaddrs(); err != nil {
			return nil, fmt.Errorf(
				"%w: %w",
				errServiceConnection, err,
			)
		}
	}
	client, err := connect(serviceMaddrs, options...)
	if err == nil {
		return client, nil
	}
	if autoLaunchDaemon &&
		errors.Is(err, errCouldNotDial) {
		return launchAndConnect(cs.exitInterval, options...)
	}
	return nil, err
}

func (co *clientOptions) BindFlags(flagSet *flag.FlagSet) {
	const (
		verboseName  = "verbose"
		verboseUsage = "enable client message logging"
	)
	flagSetFunc(flagSet, verboseName, verboseUsage, co,
		func(verbose bool, settings *clientSettings) error {
			if verbose {
				const (
					prefix = "⬇️ client - "
					flags  = 0
				)
				settings.log = log.New(os.Stderr, prefix, flags)
			}
			return nil
		})
	const (
		exitName  = exitAfterFlagName
		exitUsage = "passed to the daemon command if we launch it" +
			"\n(refer to daemon's helptext)"
	)
	flagSetFunc(flagSet, exitName, exitUsage, co,
		func(value time.Duration, settings *clientSettings) error {
			settings.exitInterval = value
			return nil
		})
	flagSet.Lookup(exitName).
		DefValue = exitIntervalDefault.String()
	const serverUsage = "file system service `maddr`"
	flagSetFunc(flagSet, serverFlagName, serverUsage, co,
		func(value multiaddr.Multiaddr, settings *clientSettings) error {
			settings.serviceMaddr = value
			return nil
		})
	serviceMaddrs, err := allServiceMaddrs()
	if err != nil {
		panic(err)
	}
	maddrStrings := make([]string, len(serviceMaddrs))
	for i, maddr := range serviceMaddrs {
		maddrStrings[i] = "`" + maddr.String() + "`"
	}
	flagSet.Lookup(serverFlagName).
		DefValue = fmt.Sprintf(
		"one of: %s",
		strings.Join(maddrStrings, ", "),
	)
}

func (co clientOptions) make() (clientSettings, error) {
	settings := clientSettings{
		exitInterval: exitIntervalDefault,
	}
	if err := generic.ApplyOptions(&settings, co...); err != nil {
		return clientSettings{}, err
	}
	return settings, nil
}

func (c *Client) getListeners() ([]multiaddr.Multiaddr, error) {
	listenersDir, err := (*p9.Client)(c).Attach(listenersFileName)
	if err != nil {
		return nil, err
	}
	maddrs, err := p9fs.GetListeners(listenersDir)
	if err != nil {
		return nil, errors.Join(err, listenersDir.Close())
	}
	return maddrs, listenersDir.Close()
}

func launchAndConnect(exitInterval time.Duration, options ...p9.ClientOpt) (*Client, error) {
	daemon, ipc, stderr, err := spawnDaemonProc(exitInterval)
	if err != nil {
		return nil, err
	}
	killProc := func() error {
		return errors.Join(
			maybeKill(daemon),
			daemon.Process.Release(),
		)
	}
	maddrs, err := getListenersFromProc(ipc, stderr, options...)
	if err != nil {
		errs := []error{err}
		if err := killProc(); err != nil {
			errs = append(errs, err)
		}
		return nil, errors.Join(errs...)
	}
	client, err := connect(maddrs)
	if err != nil {
		return nil, errors.Join(err, killProc())
	}
	if err := daemon.Process.Release(); err != nil {
		// We can no longer call `Kill`, and stdio
		// IPC is closed. Attempt to abort the service
		// via the established socket connection.
		errs := []error{err}
		if err := client.Shutdown(immediateShutdown); err != nil {
			errs = append(errs, err)
		}
		if err := client.Close(); err != nil {
			errs = append(errs, err)
		}
		return nil, errors.Join(errs...)
	}
	return client, nil
}

func Connect(serverMaddr multiaddr.Multiaddr, options ...p9.ClientOpt) (*Client, error) {
	return connect([]multiaddr.Multiaddr{serverMaddr}, options...)
}

func connect(maddrs []multiaddr.Multiaddr, options ...p9.ClientOpt) (*Client, error) {
	conn, err := firstDialable(maddrs...)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %w",
			errServiceConnection, err,
		)
	}
	return newClient(conn, options...)
}

func newClient(conn io.ReadWriteCloser, options ...p9.ClientOpt) (*Client, error) {
	client, err := p9.NewClient(conn, options...)
	if err != nil {
		return nil, fmt.Errorf(
			"could not create client: %w",
			err,
		)
	}
	return (*Client)(client), nil
}

func allServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	var (
		userMaddrs, uErr   = userServiceMaddrs()
		systemMaddrs, sErr = systemServiceMaddrs()
		serviceMaddrs      = append(userMaddrs, systemMaddrs...)
	)
	if err := errors.Join(uErr, sErr); err != nil {
		return nil, fmt.Errorf(
			"could not retrieve service maddrs: %w",
			err,
		)
	}
	return serviceMaddrs, nil
}

// TODO: [Ame] docs.
// userServiceMaddrs returns a list of multiaddrs that servers and client commands
// may try to use when hosting or querying a user-level file system service.
func userServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	return servicePathsToServiceMaddrs(xdg.StateHome, xdg.RuntimeDir)
}

// TODO: [Ame] docs.
// systemServiceMaddrs returns a list of multiaddrs that servers and client commands
// may try to use when hosting or querying a system-level file system service.
func systemServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	return hostServiceMaddrs()
}

func firstDialable(maddrs ...multiaddr.Multiaddr) (manet.Conn, error) {
	for _, maddr := range maddrs {
		if conn, err := manet.Dial(maddr); err == nil {
			return conn, nil
		}
	}
	return nil, fmt.Errorf(
		"%w any of: %s",
		errCouldNotDial,
		formatMaddrs(maddrs),
	)
}

func formatMaddrs(maddrs []multiaddr.Multiaddr) string {
	maddrStrings := make([]string, len(maddrs))
	for i, maddr := range maddrs {
		maddrStrings[i] = maddr.String()
	}
	return strings.Join(maddrStrings, ", ")
}

func servicePathsToServiceMaddrs(servicePaths ...string) ([]multiaddr.Multiaddr, error) {
	var (
		serviceMaddrs = make([]multiaddr.Multiaddr, 0, len(servicePaths))
		multiaddrSet  = make(map[string]struct{}, len(servicePaths))
	)
	for _, servicePath := range servicePaths {
		if _, alreadySeen := multiaddrSet[servicePath]; alreadySeen {
			continue // Don't return duplicates in our slice.
		}
		multiaddrSet[servicePath] = struct{}{}
		var (
			nativePath        = filepath.Join(servicePath, serverRootName, serverName)
			serviceMaddr, err = filepathToUnixMaddr(nativePath)
		)
		if err != nil {
			return nil, err
		}
		serviceMaddrs = append(serviceMaddrs, serviceMaddr)
	}
	return serviceMaddrs, nil
}

func filepathToUnixMaddr(nativePath string) (multiaddr.Multiaddr, error) {
	const (
		protocolPrefix = "/unix"
		unixNamespace  = len(protocolPrefix)
		slash          = 1
	)
	var (
		insertSlash = !strings.HasPrefix(nativePath, "/")
		size        = unixNamespace + len(nativePath)
	)
	if insertSlash {
		size += slash
	}
	// The component's protocol's value should be concatenated raw,
	// with platform native conventions. I.e. avoid [path.Join].
	// For non-Unix formatted filepaths, we'll need to insert the multiaddr delimiter.
	var maddrBuilder strings.Builder
	maddrBuilder.Grow(size)
	maddrBuilder.WriteString(protocolPrefix)
	if insertSlash {
		maddrBuilder.WriteRune('/')
	}
	maddrBuilder.WriteString(nativePath)
	return multiaddr.NewMultiaddr(maddrBuilder.String())
}

func (c *Client) Close() error {
	return (*p9.Client)(c).Close()
}
