package fscmds

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"

	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/filesystem/manager"
	cmds "github.com/ipfs/go-ipfs-cmds"
	cmdshttp "github.com/ipfs/go-ipfs-cmds/http"
	config "github.com/ipfs/go-ipfs-config"
	httpapi "github.com/ipfs/go-ipfs-http-client"
	"github.com/ipfs/go-ipfs/repo/fsrepo"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	"github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (

	// TODO: we might not want to export this
	// the interface needs review as well
	// Service methods unclear right now
	// maybe should take request, maybe return error, maybe just be type assertions inside run

	// FileSystemEnvironment must be implemented by the env passed to `FileSystem`'s `PreRun` and `Run` methods.
	FileSystemEnvironment interface {
		// environment storage
		Manager(*cmds.Request) (manager.Interface, error)
		Index(*cmds.Request) (manager.Index, error)

		// typically used by the cmds executor constructor
		SystemService() (cmds.Executor, error)

		// typically used by the cmds run methods
		//ServiceMaddr() multiaddr.Multiaddr
		IPFS(*cmds.Request) (coreiface.CoreAPI, error)
	}
	filesystemEnvironment struct {
		context.Context
		instanceIndex

		serviceMaddr  multiaddr.Multiaddr
		serviceClient cmds.Executor
	}
)

// TODO: assert at compile time in test that struct implements interface
// normally the return type would do that but cmds.Environment is required to be used in cli.Run
var _ FileSystemEnvironment = (*filesystemEnvironment)(nil)

func MakeFileSystemEnvironment(ctx context.Context, request *cmds.Request) (env cmds.Environment, err error) {
	var ( // environment storage
		fsEnv = &filesystemEnvironment{
			instanceIndex: newIndex(),
			Context:       ctx,
		}
	)

	var (
		rctx            = request.Context
		settings        = new(settings)
		unsetArgs, errs = parameters.ParseSettings(rctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
	)
	if _, err = parameters.AccumulateArgs(ctx, unsetArgs, errs); err != nil {
		return
	}

	// HACK: needs proper something something
	if len(settings.ServiceMaddrs) == 0 {
		settings.ServiceMaddrs, _ = fscmds.UserServiceMaddrs()
	}

	// resolve the address contained in the request
	serviceMaddr := settings.ServiceMaddrs[0] // TODO [port]: this became a vector.
	if serviceMaddr, err = resolveAddr(request.Context, serviceMaddr); err != nil {
		return
	}

	fsEnv.serviceMaddr = serviceMaddr
	env = fsEnv
	return
}

// getServiceClient will test the connection before returning the client
func getServiceClient(serviceMaddr multiaddr.Multiaddr) (cmds.Executor, error) {
	network, dialHost, clientHost, clientOpts, err := parseClientOptions(serviceMaddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial(network, dialHost)
	if err == nil { // use this address
		if err = conn.Close(); err != nil {
			return nil, err
		}
		return cmdshttp.NewClient(clientHost, clientOpts...), nil
	}
	if network == "unix" {
		if _, err := os.Stat(dialHost); err == nil {
			os.Remove(dialHost)
		}
	}
	return nil, fmt.Errorf("could not connect to service daemon: %w", err)
}

func parseClientOptions(maddr multiaddr.Multiaddr) (network, dialHost, clientHost string, clientOpts []cmdshttp.ClientOpt, err error) {
	if network, dialHost, err = manet.DialArgs(maddr); err != nil {
		return
	}
	switch network {
	case "tcp", "tcp4", "tcp6":
		clientHost = dialHost
	case "unix":
		// TODO: consider patching cmds-lib
		// we want to use the URL scheme "http+unix"
		// as-is, it prefixes the value to be parsed by pkg URL as "http://http+unix://"
		clientHost = "http://file-system-socket" // TODO: const + needs real name/value
		netDialer := new(net.Dialer)
		clientOpts = append(clientOpts, cmdshttp.ClientWithHTTPClient(&http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return netDialer.DialContext(ctx, network, dialHost)
				},
			},
		}))
	default:
		err = fmt.Errorf("unsupported API address: %s", maddr)
	}
	return
}

func (fe *filesystemEnvironment) Manager(request *cmds.Request) (manager.Interface, error) {
	ipfs, err := fe.IPFS(request)
	if err != nil {
		return nil, err
	}

	ipfsDispatch, err := newCoreDispatchers(fe.Context, ipfs)
	if err != nil {
		return nil, err
	}

	return &commandDispatcher{
		instanceIndex: fe.instanceIndex,
		dispatchers:   ipfsDispatch,
	}, nil
}

func (fe *filesystemEnvironment) IPFS(request *cmds.Request) (coreiface.CoreAPI, error) {
	apiAddr, err := getIPFSAPIAddr(request)
	if err != nil {
		return nil, err
	}
	if apiAddr, err = resolveAddr(request.Context, apiAddr); err != nil {
		return nil, err
	}
	return httpapi.NewApi(apiAddr)
}

type settings struct {
	fscmds.Settings
	IPFS multiaddr.Multiaddr `settings:"arguments"`
}

func (*settings) Parameters() parameters.Parameters {
	root := fscmds.Parameters()
	hack := []parameters.Parameter{
		parameters.NewParameter("IPFS maddr",
			parameters.WithNamespace("mount"),
			parameters.WithName("ipfs"),
		),
	}
	return append(root, hack...)
}

func getIPFSAPIAddr(request *cmds.Request) (multiaddr.Multiaddr, error) {
	// TODO: port wart - migrate properly to new params lib
	// and probably move this logic into mount cmd anyway.
	var (
		ctx             = request.Context
		settings        = new(settings)
		unsetArgs, errs = parameters.ParseSettings(ctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
		_, err = parameters.AccumulateArgs(ctx, unsetArgs, errs)
	)
	if err != nil {
		return nil, err
	}

	// (precedence 0) parameters
	apiAddr := settings.IPFS
	if apiAddr != nil {
		return apiAddr, nil
	}

	// (precedence 1) (maybe) IPFS config file
	confRoot, err := config.PathRoot()
	if err == nil {
		apiAddr, err = fsrepo.APIAddr(confRoot)
	}
	return apiAddr, err
}

func (fe *filesystemEnvironment) Index(request *cmds.Request) (manager.Index, error) {
	return fe.instanceIndex, nil
}

func (fe *filesystemEnvironment) SystemService() (cmds.Executor, error) {
	if fe.serviceClient != nil {
		return fe.serviceClient, nil
	}
	exe, err := getServiceClient(fe.serviceMaddr)
	if err == nil {
		fe.serviceClient = exe
	}
	return exe, err
}

func (fe *filesystemEnvironment) ServiceMaddr() multiaddr.Multiaddr { return fe.serviceMaddr }

func envError(env cmds.Environment) error {
	return cmds.Errorf(cmds.ErrClient,
		"expected environment of type %T but received %T", (*filesystemEnvironment)(nil), env)
}

func resolveAddr(ctx context.Context, addr multiaddr.Multiaddr) (multiaddr.Multiaddr, error) {
	ctx, cancelFunc := context.WithTimeout(ctx, 10*time.Second)
	defer cancelFunc()

	addrs, err := madns.DefaultResolver.Resolve(ctx, addr)
	if err != nil {
		return nil, err
	}

	if len(addrs) == 0 {
		return nil, errors.New("non-resolvable API endpoint")
	}

	return addrs[0], nil
}
