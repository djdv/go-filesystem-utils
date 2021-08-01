package fscmds

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	cmds "github.com/ipfs/go-ipfs-cmds"
	cmdshttp "github.com/ipfs/go-ipfs-cmds/http"
	httpapi "github.com/ipfs/go-ipfs-http-client"
	"github.com/ipfs/go-ipfs/filesystem/manager"
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
		ServiceMaddr() multiaddr.Multiaddr
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
		serviceMaddr multiaddr.Multiaddr
	)
	if serviceMaddr, err = multiaddrOption(request, rootServiceOptionKwd); err != nil {
		return
	}

	var ( // request conditions
		isLocalCommand   = !request.Command.NoLocal || request.Command.NoRemote
		addrWasProvided  = serviceMaddr != nil
		tryLaunchService = !isLocalCommand && !addrWasProvided
	)
	if !addrWasProvided {
		if serviceMaddr, err = localServiceMaddr(); err != nil {
			return
		}
	} else { // resolve the address contained in the request
		if serviceMaddr, err = resolveAddr(request.Context, serviceMaddr); err != nil {
			return
		}
	}

	// TODO: check for environment variable ?
	// ${FS_API} ???

	fsEnv.serviceMaddr = serviceMaddr

	if tryLaunchService {
		// try connecting
		if _, err = getServiceClient(serviceMaddr); err != nil {
			// not okay; try to launch service
			_, err = relaunchSelfAsService(request, serviceMaddr)
		}
	}
	if err == nil {
		env = fsEnv
	}
	return
}

func MakeFileSystemExecutor(request *cmds.Request, env interface{}) (cmds.Executor, error) {
	fsEnv, envIsUsable := env.(FileSystemEnvironment)
	if !envIsUsable {
		return nil, envError(env)
	}

	// execute the request locally if we can
	if !request.Command.NoLocal || request.Command.NoRemote {
		return cmds.NewExecutor(request.Root), nil
	}
	// everything else connects to a cmds server as a client
	return fsEnv.SystemService()
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
