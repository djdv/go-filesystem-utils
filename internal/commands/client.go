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
)

type (
	Client         p9.Client
	clientSettings struct {
		serviceMaddr multiaddr.Multiaddr
		exitInterval time.Duration
		sharedSettings
	}
	clientOption  func(*clientSettings) error
	clientOptions []clientOption
	// defaultClientMaddr distinguishes
	// the default maddr value, from an arbitrary maddr value.
	// I.e. even if the underlying multiaddrs are the same
	// only the flag's default value should be of this type.
	// Implying the flag was not provided/set explicitly.
	defaultClientMaddr struct{ multiaddr.Multiaddr }
)

const (
	exitIntervalDefault = 30 * time.Second
	errServiceNotFound  = generic.ConstError("could not find service instance")
)

func (cs *clientSettings) getClient(autoLaunchDaemon bool) (*Client, error) {
	var (
		serviceMaddr = cs.serviceMaddr
		clientOpts   []p9.ClientOpt
	)
	if cs.verbose {
		// TODO: less fancy prefix and/or out+prefix from CLI flags
		clientLog := log.New(os.Stdout, "⬇️ client - ", log.Lshortfile)
		clientOpts = append(clientOpts, p9.WithClientLogger(clientLog))
	}
	if autoLaunchDaemon {
		if _, wasUnset := serviceMaddr.(defaultClientMaddr); wasUnset {
			return connectOrLaunchLocal(cs.exitInterval, clientOpts...)
		}
	}
	return Connect(serviceMaddr, clientOpts...)
}

func (co *clientOptions) BindFlags(flagSet *flag.FlagSet) {
	var sharedOptions sharedOptions
	(&sharedOptions).BindFlags(flagSet)
	*co = append(*co, func(cs *clientSettings) error {
		subset, err := sharedOptions.make()
		if err != nil {
			return err
		}
		cs.sharedSettings = subset
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
	flagSet.Lookup(serverFlagName).
		DefValue = defaultServerMaddr().String()
}

func (co clientOptions) make() (clientSettings, error) {
	settings := clientSettings{
		exitInterval: exitIntervalDefault,
	}
	if err := generic.ApplyOptions(&settings, co...); err != nil {
		return clientSettings{}, err
	}
	if err := settings.fillDefaults(); err != nil {
		return clientSettings{}, err
	}
	return settings, nil
}

func (cs *clientSettings) fillDefaults() error {
	if cs.serviceMaddr == nil {
		cs.serviceMaddr = defaultServerMaddr()
	}
	return nil
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

func connectOrLaunchLocal(exitInterval time.Duration, options ...p9.ClientOpt) (*Client, error) {
	conn, err := findLocalServer()
	if err == nil {
		return newClient(conn, options...)
	}
	if !errors.Is(err, errServiceNotFound) {
		return nil, err
	}
	return launchAndConnect(exitInterval, options...)
}

func launchAndConnect(exitInterval time.Duration, options ...p9.ClientOpt) (*Client, error) {
	daemon, ipc, stderr, err := spawnDaemonProc(exitInterval)
	if err != nil {
		return nil, err
	}
	var (
		errs     []error
		killProc = func() error {
			return errors.Join(
				maybeKill(daemon),
				daemon.Process.Release(),
			)
		}
	)
	maddrs, err := getListenersFromProc(ipc, stderr, options...)
	if err != nil {
		errs = append(errs, err)
		if err := killProc(); err != nil {
			errs = append(errs, err)
		}
		return nil, errors.Join(errs...)
	}
	conn, err := firstDialable(maddrs)
	if err != nil {
		return nil, errors.Join(err, killProc())
	}
	client, err := newClient(conn, options...)
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
	conn, err := manet.Dial(serverMaddr)
	if err != nil {
		return nil, fmt.Errorf("could not connect to service: %w", err)
	}
	return newClient(conn, options...)
}

func newClient(conn io.ReadWriteCloser, options ...p9.ClientOpt) (*Client, error) {
	client, err := p9.NewClient(conn, options...)
	if err != nil {
		return nil, err
	}
	return (*Client)(client), nil
}

// findLocalServer searches a set of local addresses
// and returns the first dialable maddr it finds.
// [errServiceNotFound] will be returned if none are dialable.
func findLocalServer() (manet.Conn, error) {
	allMaddrs, err := allServiceMaddrs()
	if err != nil {
		return nil, err
	}
	return firstDialable(allMaddrs)
}

func allServiceMaddrs() ([]multiaddr.Multiaddr, error) {
	var (
		userMaddrs, uErr   = userServiceMaddrs()
		systemMaddrs, sErr = systemServiceMaddrs()
		serviceMaddrs      = append(userMaddrs, systemMaddrs...)
	)
	return serviceMaddrs, errors.Join(uErr, sErr)
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

func firstDialable(maddrs []multiaddr.Multiaddr) (manet.Conn, error) {
	for _, maddr := range maddrs {
		if conn, err := manet.Dial(maddr); err == nil {
			return conn, nil
		}
	}
	maddrStrings := make([]string, len(maddrs))
	for i, maddr := range maddrs {
		maddrStrings[i] = maddr.String()
	}
	var (
		cErr      error = errServiceNotFound
		fmtString       = strings.Join(maddrStrings, ", ")
	)
	return nil, fmt.Errorf("%w: tried: %s", cErr, fmtString)
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
