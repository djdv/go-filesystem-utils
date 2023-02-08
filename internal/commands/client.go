package commands

import (
	"errors"
	"flag"
	"fmt"
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
	"github.com/u-root/uio/ulog"
)

type (
	nanoidGen = func() string
	Client    struct {
		p9Client *p9.Client
		log      ulog.Logger
		idGen    nanoidGen // TODO: review; this might not need to be dynamic. Or even part of the client.
	}
	ClientOption   func(*Client) error
	clientSettings struct {
		serviceMaddr multiaddr.Multiaddr
		commonSettings
		daemonDecay
	}
	// defaultClientMaddr distinguishes
	// the default maddr value, from an arbitrary maddr value.
	// I.e. even if the underlying multiaddrs are the same
	// only the flag's default value should be of this type.
	// Implying the flag was not provided/set explicitly.
	defaultClientMaddr struct{ multiaddr.Multiaddr }
)

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
	defaultText := map[string]string{
		sockName: sockDefaultText,
	}
	flagSet.VisitAll(func(f *flag.Flag) {
		if text, ok := defaultText[f.Name]; ok {
			f.DefValue = text
		}
	})
}

const (
	idLength       = 9
	base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

	ErrCouldNotConnect = generic.ConstError("could not connect to remote API")
	ErrServiceNotFound = generic.ConstError("could not find service instance")
)

func WithLogger(log ulog.Logger) ClientOption {
	return func(c *Client) error { c.log = log; return nil }
}

func getClient(set *clientSettings, autoLaunchDaemon bool) (*Client, error) {
	var (
		serviceMaddr = set.serviceMaddr
		clientOpts   []ClientOption
	)
	if set.verbose {
		// TODO: less fancy prefix and/or out+prefix from CLI flags
		clientLog := log.New(os.Stdout, "⬇️ client - ", log.Lshortfile)
		clientOpts = append(clientOpts, WithLogger(clientLog))
	}
	if autoLaunchDaemon {
		return ConnectOrLaunchLocal(clientOpts...)
	}
	return Connect(serviceMaddr, clientOpts...)
}

func ConnectOrLaunchLocal(options ...ClientOption) (*Client, error) {
	conn, err := findindLocalServer()
	if err == nil {
		return newClient(conn, options...)
	}
	if !errors.Is(err, ErrServiceNotFound) {
		return nil, err
	}
	// TODO: const for daemon CLI cmd name
	return SelfConnect([]string{"daemon"}, options...)
}

// TODO: name is misleading,
// this launches and connects to self.
// The launching logic should go into its caller.
func SelfConnect(args []string, options ...ClientOption) (*Client, error) {
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
	const fsysName = "listeners" // TODO: magic string should probably be elsewhere. From Options?
	cmdMaddrs, err := getListenersFrom(cmdIO, fsysName)
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

func Connect(serverMaddr multiaddr.Multiaddr, options ...ClientOption) (*Client, error) {
	conn, err := manet.Dial(serverMaddr)
	if err != nil {
		return nil, err
	}
	return newClient(conn, options...)
}

func newClient(conn manet.Conn, options ...ClientOption) (*Client, error) {
	client := Client{
		log: ulog.Null,
	}
	for _, setFunc := range options {
		if err := setFunc(&client); err != nil {
			panic(err)
		}
	}
	var (
		err        error
		clientOpts = []p9.ClientOpt{
			p9.WithClientLogger(client.log),
		}
	)
	if client.p9Client, err = p9.NewClient(conn, clientOpts...); err != nil {
		return nil, err
	}
	return &client, nil
}

// findindLocalServer searches a set of local addresses
// and returns the first dialable maddr it finds.
// [ErrServiceNotFound] will be returned if none are dialable.
func findindLocalServer() (manet.Conn, error) {
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
	cl := c.p9Client
	if cl == nil {
		// TODO: better message; maybe better logic?
		// Can we prevent this from being possible without unexporting [Client]?
		return fmt.Errorf("client is not connected")
	}
	c.p9Client = nil
	return cl.Close()
}
