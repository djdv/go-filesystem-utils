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
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	Client         p9.Client
	clientSettings struct {
		serviceMaddr multiaddr.Multiaddr
		exitInterval time.Duration
		commonSettings
	}
	// defaultClientMaddr distinguishes
	// the default maddr value, from an arbitrary maddr value.
	// I.e. even if the underlying multiaddrs are the same
	// only the flag's default value should be of this type.
	// Implying the flag was not provided/set explicitly.
	defaultClientMaddr struct{ multiaddr.Multiaddr }
)

func (set *clientSettings) getClient(autoLaunchDaemon bool) (*Client, error) {
	var (
		serviceMaddr = set.serviceMaddr
		clientOpts   []p9.ClientOpt
	)
	if set.verbose {
		// TODO: less fancy prefix and/or out+prefix from CLI flags
		clientLog := log.New(os.Stdout, "⬇️ client - ", log.Lshortfile)
		clientOpts = append(clientOpts, p9.WithClientLogger(clientLog))
	}
	if autoLaunchDaemon {
		if _, wasUnset := serviceMaddr.(defaultClientMaddr); wasUnset {
			return connectOrLaunchLocal(clientOpts...)
		}
	}
	return Connect(serviceMaddr, clientOpts...)
}

func (set *clientSettings) BindFlags(flagSet *flag.FlagSet) {
	set.commonSettings.BindFlags(flagSet)
	const (
		exitFlag  = exitAfterFlagName
		exitUsage = "passed to the daemon command if we launch it\nrefer to daemon's helptext"
	)
	flagSet.DurationVar(&set.exitInterval, exitFlag, 0, exitUsage)
	const (
		sockName  = serverFlagName
		sockUsage = "file system service `maddr`"
	)
	var sockDefaultText string
	{
		maddrs, err := userServiceMaddrs()
		if err != nil {
			panic(err)
		}
		sockDefault := maddrs[0]
		sockDefaultText = sockDefault.String()
		set.serviceMaddr = defaultClientMaddr{sockDefault}
	}
	flagSet.Func(sockName, sockUsage, func(s string) (err error) {
		set.serviceMaddr, err = multiaddr.NewMultiaddr(s)
		return
	})
	setDefaultValueText(flagSet, flagDefaultText{
		sockName: sockDefaultText,
	})
}

const ErrServiceNotFound = generic.ConstError("could not find service instance")

func connectOrLaunchLocal(options ...p9.ClientOpt) (*Client, error) {
	conn, err := findLocalServer()
	if err == nil {
		return newClient(conn, options...)
	}
	if !errors.Is(err, ErrServiceNotFound) {
		return nil, err
	}
	return selfConnect([]string{daemonCommandName}, options...)
}

// TODO: name is misleading,
// this launches and connects to self.
// The launching logic should go into its caller.
func selfConnect(args []string, options ...p9.ClientOpt) (*Client, error) {
	// TODO: should be a ClientOption value?
	// argument?
	const defaultDecay = 30 * time.Second
	cmd, err := selfCommand(args, defaultDecay)
	if err != nil {
		return nil, err
	}
	cmdIO, err := setupCmdIPC(cmd)
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// NOTE: Order of operations is important here.
	// 1) We must always close IO when done with the subprocess.
	// Otherwise either process could block on IO operations.
	// 2) The subprocess handle must remain held if it is to be killed.
	// PIDs may be reused by other processes if they're released from us and the process exits.
	// 3) The subprocess must be released before returning.
	// We must not terminate the subprocess when our process terminates.
	abort := func() error {
		return fserrors.Join(maybeKill(cmd), cmd.Process.Release())
	}
	cmdMaddrs, err := getListenersFrom(cmdIO, listenersFileName)
	err = fserrors.Join(err, cmdIO.Close())
	if err != nil {
		return nil, fserrors.Join(err, abort())
	}
	if len(cmdMaddrs) == 0 {
		var cErr error = ErrServiceNotFound
		err = fmt.Errorf("%w: daemon didn't return any addresses", cErr)
		return nil, fserrors.Join(err, abort())
	}
	conn, err := firstDialable(cmdMaddrs)
	if err != nil {
		return nil, fserrors.Join(err, abort())
	}
	if err := fserrors.Join(err, cmd.Process.Release()); err != nil {
		return nil, err
	}
	return newClient(conn, options...)
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
// [ErrServiceNotFound] will be returned if none are dialable.
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
	return serviceMaddrs, fserrors.Join(uErr, sErr)
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
		cErr      error = ErrServiceNotFound
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
