package ipc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"reflect"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/filesystem/cgofuse"
	ipfs "github.com/djdv/go-filesystem-utils/filesystem/ipfscore"
	"github.com/djdv/go-filesystem-utils/filesystem/pinfs"
	cmds "github.com/ipfs/go-ipfs-cmds"
	ipfsconfig "github.com/ipfs/go-ipfs-config"
	ipfsconfigfile "github.com/ipfs/go-ipfs-config/serialize"
	httpapi "github.com/ipfs/go-ipfs-http-client"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	"github.com/kardianos/service"
	"github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	Environment interface {
		ServiceConfig(*cmds.Request) (*service.Config, error)
		Mount(*cmds.Request) ([]filesystem.MountPoint, error)
		List(*cmds.Request) ([]multiaddr.Multiaddr, error)
		Unmount(*cmds.Request) ([]multiaddr.Multiaddr, error)
	}

	fsidMap     map[filesystem.ID]fs.FS
	ipfsBinding struct {
		client  coreiface.CoreAPI
		systems fsidMap
	}

	binderPair struct {
		fsid       filesystem.ID
		identifier string
	}
	binderMap map[binderPair]filesystem.Mounter

	maddrString = string
	ipfsMap     map[maddrString]*ipfsBinding
	instanceMap map[string]filesystem.MountPoint

	daemonEnvironment struct {
		context.Context
		ipfsBindings  ipfsMap
		hostBinders   binderMap
		hostInstances instanceMap
	}
)

func MakeEnvironment(ctx context.Context, request *cmds.Request) (cmds.Environment, error) {
	env := &daemonEnvironment{
		Context: ctx,
	}
	return env, nil
}

func CastEnvironment(environment cmds.Environment) (Environment, error) {
	typedEnv, isUsable := environment.(Environment)
	if !isUsable {
		interfaceName := reflect.TypeOf((*Environment)(nil)).Elem().Name()
		return nil, cmds.Errorf(cmds.ErrImplementation,
			"%T does not implement the %s interface",
			environment, interfaceName,
		)
	}
	return typedEnv, nil
}

func ipfsMaddrFromConfig() (multiaddr.Multiaddr, error) {
	confFile, err := ipfsconfig.Filename("") // TODO: argument from CLI?
	if err != nil {
		return nil, err
	}
	nodeConf, err := ipfsconfigfile.Load(confFile)
	if err != nil {
		return nil, err
	}

	// TODO: we should probably try dialing each of these and using the first to respond
	apiMaddrs := nodeConf.Addresses.API
	if len(apiMaddrs) == 0 {
		// TODO: real message
		return nil, errors.New("config has no API addrs")
	}

	return multiaddr.NewMultiaddr(apiMaddrs[0])
}

func resolveMaddr(ctx context.Context, addr multiaddr.Multiaddr) (multiaddr.Multiaddr, error) {
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

func ipfsClient(apiMaddr multiaddr.Multiaddr) (coreiface.CoreAPI, error) {
	ctx, cancelFunc := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelFunc()
	resolvedMaddr, err := resolveMaddr(ctx, apiMaddr)
	if err != nil {
		return nil, err
	}

	// TODO: I think the upstream package needs a patch to handle this internally.
	// we'll hack around it for now. Investigate later.
	// (When trying to use a unix socket for the IPFS maddr
	// the client returned from httpapi.NewAPI will complain on requests - forgot to copy the error lol)
	network, dialHost, err := manet.DialArgs(resolvedMaddr)
	if err != nil {
		return nil, err
	}
	switch network {
	default:
		return httpapi.NewApi(resolvedMaddr)
	case "unix":
		// TODO: consider patching cmds-lib
		// we want to use the URL scheme "http+unix"
		// as-is, it prefixes the value to be parsed by pkg `url` as "http://http+unix://"
		var (
			clientHost = "http://file-system-socket" // TODO: const + needs real name/value
			netDialer  = new(net.Dialer)
		)
		return httpapi.NewURLApiWithClient(clientHost, &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return netDialer.DialContext(ctx, network, dialHost)
				},
			},
		})
	}
}

// TODO: [review] I hate all this map allocation business. See if we can simplify.
// TODO: mutex concerns on map access when called from 2 processes at once.
func (env *daemonEnvironment) getIPFS(fsid filesystem.ID, ipfsMaddr multiaddr.Multiaddr) (fs.FS, error) {
	bindings := env.ipfsBindings
	if bindings == nil {
		bindings = make(ipfsMap)
		env.ipfsBindings = bindings
	}
	var (
		nodeMaddr = ipfsMaddr.String()
		binding   = bindings[nodeMaddr]
	)
	if binding == nil {
		core, err := ipfsClient(ipfsMaddr)
		if err != nil {
			return nil, err
		}
		binding = &ipfsBinding{client: core, systems: make(fsidMap)}
		bindings[nodeMaddr] = binding
	}

	fileSystem := binding.systems[fsid]
	if fileSystem == nil {
		ctx := env.Context
		switch fsid {
		case filesystem.IPFS,
			filesystem.IPNS:
			fileSystem = ipfs.NewInterface(ctx, binding.client, fsid)
		case filesystem.PinFS:
			fileSystem = pinfs.NewInterface(ctx, binding.client)
		default:
			return nil, errors.New("TODO: real msg - fsid not supported")
		}
		binding.systems[fsid] = fileSystem
	}
	return fileSystem, nil
}

func (env *daemonEnvironment) getFuse(bindKey binderPair, fileSystem fs.FS) (filesystem.Mounter, error) {
	binders := env.hostBinders
	if binders == nil {
		binders = make(binderMap)
		env.hostBinders = binders
	}

	binder := binders[bindKey]
	if binder == nil {
		var err error
		if binder, err = cgofuse.NewMounter(env.Context, fileSystem); err != nil {
			return nil, err
		}
	}

	return binder, nil
}

// lazy-alloc boilerplate below
// TODO: mutexes; multiple processes may communicate with the daemon at once.

func (m *binderMap) Add(pair binderPair, mounter filesystem.Mounter) {
	binder := *m
	if binder == nil {
		binder = make(binderMap)
		*m = binder
	}
	binder[pair] = mounter
}

func (m binderMap) Get(pair binderPair) filesystem.Mounter { return m[pair] }

func (m *instanceMap) Add(maddr multiaddr.Multiaddr, mountPoint filesystem.MountPoint) {
	mountPoints := *m
	if mountPoints == nil {
		mountPoints = make(instanceMap)
		*m = mountPoints
	}
	mountPoints[maddr.String()] = mountPoint
}

// TODO: lint - this needs to be List() []... - mutex guarded
//func (m instanceMap) Get(maddr multiaddr.Multiaddr) filesystem.MountPoint { return m[maddr.String()] }

func (m *instanceMap) Close(maddr multiaddr.Multiaddr) error {
	mounts := *m
	mountPoint := mounts[maddr.String()]
	if mountPoint == nil {
		return fmt.Errorf("TODO: real msg - can't close \"%s\", no instance found",
			maddr,
		)
	}
	err := mountPoint.Close()
	// NOTE: We stop tracking this mountPoint
	// whether Close succeeds or not.
	// If the instance did not detach from the host,
	// we can only defer to the system operator to do it themselves.
	// (`umount -f`, etc. depending on the platform)
	delete(mounts, maddr.String())
	return err
}
