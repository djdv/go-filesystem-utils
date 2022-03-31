package environment

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"time"

	"github.com/djdv/go-filesystem-utils/cmd/fs/settings"
	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/filesystem/cgofuse"
	ipfs "github.com/djdv/go-filesystem-utils/filesystem/ipfscore"
	"github.com/djdv/go-filesystem-utils/filesystem/keyfs"
	"github.com/djdv/go-filesystem-utils/filesystem/pinfs"
	cmds "github.com/ipfs/go-ipfs-cmds"
	ipfsconfig "github.com/ipfs/go-ipfs-config"
	ipfsconfigfile "github.com/ipfs/go-ipfs-config/serialize"
	httpapi "github.com/ipfs/go-ipfs-http-client"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	"github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	Mounter interface {
		Mount(request *cmds.Request) ([]filesystem.MountPoint, error)
		Unmount(request *cmds.Request) ([]multiaddr.Multiaddr, error)
		// TODO: options parameter: ...opts
		// parse: cmds.Request's `-a` => list(listOptAll(true))
		// TODO: List should take a context.
		List() (<-chan multiaddr.Multiaddr, error)
	}

	instanceMap map[string]filesystem.MountPoint

	binderPair struct {
		identifier string
		fsid       filesystem.ID
	}
	binderMap map[binderPair]filesystem.Mounter

	fsidMap     map[filesystem.ID]fs.FS
	ipfsBinding struct {
		client  coreiface.CoreAPI
		systems fsidMap
	}
	maddrString = string
	ipfsMap     map[maddrString]*ipfsBinding

	mounter struct {
		context.Context
		hostBinders   binderMap
		hostInstances instanceMap

		ipfsBindings ipfsMap
	}
)

func (env *mounter) Mount(request *cmds.Request) ([]filesystem.MountPoint, error) {
	ctx := env.Context
	mountSettings, err := settings.ParseAll[settings.MountSettings](ctx, request)
	if err != nil {
		return nil, err
	}

	var (
		args         = request.Arguments
		targetMaddrs = make([]multiaddr.Multiaddr, len(args))
	)
	for i, target := range args {
		maddr, err := multiaddr.NewMultiaddr(target)
		if err != nil {
			return nil, err
		}
		targetMaddrs[i] = maddr
	}

	// TODO: use a dynamic default value, the one most appropriate for this platform
	var (
		host = mountSettings.HostAPI
		fsid = mountSettings.FSID
	)
	if host == 0 {
		host = filesystem.Fuse
	}
	if fsid == 0 {
		fsid = filesystem.IPFS
	}

	var (
		// TODO: Reconsider how to distinguish sets.
		// We need some kind interface for this.
		// myfs.uuid(), myfs.hashfn(somethingUnique), etc.
		// Anything to split up things like IPFS targets used
		// with the host API; but generic, not strictly a maddr.
		fileSystem fs.FS
		identifier string
	)
	switch fsid {
	case filesystem.IPFS,
		filesystem.IPNS,
		filesystem.PinFS,
		filesystem.KeyFS:
		var (
			ipfsMaddr = mountSettings.IPFSMaddr
			err       error
		)
		if ipfsMaddr == nil {
			if ipfsMaddr, err = ipfsMaddrFromConfig(); err != nil {
				return nil, err
			}
		}
		identifier = ipfsMaddr.String() // delineate via the node maddr
		if fileSystem, err = env.getIPFS(fsid, ipfsMaddr); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("TODO: real msg - fsid \"%s\" not yet supported", fsid)
	}

	var (
		mounter         filesystem.Mounter
		mountIdentifier = binderPair{fsid: fsid, identifier: identifier}
	)
	switch host {
	case filesystem.Fuse:
		var err error
		if mounter, err = env.getFuse(mountIdentifier, fileSystem); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("TODO: real msg - host API \"%s\" not yet supported", host)
	}

	mountPoints := make([]filesystem.MountPoint, 0, len(targetMaddrs))
	for _, target := range targetMaddrs {
		mountPoint, err := mounter.Mount(env.Context, target)
		if err != nil {
			for _, mountPoint := range mountPoints {
				if unmountErr := mountPoint.Close(); unmountErr != nil {
					err = fmt.Errorf("%w - %s", err, unmountErr)
				}
			}
			return nil, err
		}
		mountPoints = append(mountPoints, mountPoint)
	}

	// Only store these after success mounting above.
	for i, mountPoint := range mountPoints {
		env.hostInstances.Add(targetMaddrs[i], mountPoint)
	}

	return mountPoints, nil
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

// TODO: [review] I hate all this map allocation business. See if we can simplify.
// TODO: mutex concerns on map access when called from 2 processes at once.
func (env *mounter) getIPFS(fsid filesystem.ID, ipfsMaddr multiaddr.Multiaddr) (fs.FS, error) {
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
		case filesystem.KeyFS:
			fileSystem = keyfs.NewInterface(ctx, binding.client)
		default:
			return nil, fmt.Errorf("TODO: real msg - fsid \"%s\" not yet supported", fsid.String())
		}
		binding.systems[fsid] = fileSystem
	}
	return fileSystem, nil
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

func (env *mounter) getFuse(bindKey binderPair, fileSystem fs.FS) (filesystem.Mounter, error) {
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
// func (m instanceMap) Get(maddr multiaddr.Multiaddr) filesystem.MountPoint { return m[maddr.String()] }

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

// TODO: channel outputs
func (env *mounter) Unmount(request *cmds.Request) ([]multiaddr.Multiaddr, error) {
	ctx := request.Context
	unmountSettings, err := settings.ParseAll[settings.UnmountSettings](ctx, request)
	if err != nil {
		return nil, err
	}

	var (
		args         = request.Arguments
		targetMaddrs = make([]multiaddr.Multiaddr, len(args))
	)
	for i, target := range args {
		maddr, err := multiaddr.NewMultiaddr(target)
		if err != nil {
			return nil, err
		}
		targetMaddrs[i] = maddr
	}

	closed := make([]multiaddr.Multiaddr, 0, len(targetMaddrs))
	if unmountSettings.All {
		// TODO: alloc once
		closed = make([]multiaddr.Multiaddr, 0, len(env.hostInstances))
		// TODO: [port] make sure to prevent calling --all with args too
		for _, mountPoint := range env.hostInstances {
			target := mountPoint.Target()
			if cErr := env.hostInstances.Close(target); cErr != nil {
				if err == nil {
					err = fmt.Errorf("could not close: \"%s\" - %w", target, cErr)
				} else {
					err = fmt.Errorf("%w\n\t\"%s\" - %s", err, target, cErr)
				}
				continue
			}
			closed = append(closed, target)
		}
		return closed, err
	}
	for _, target := range targetMaddrs {
		if cErr := env.hostInstances.Close(target); cErr != nil {
			if err == nil {
				err = fmt.Errorf("could not close: \"%s\" - %w", target, cErr)
			} else {
				err = fmt.Errorf("%w\n\t\"%s\" - %s", err, target, cErr)
			}
			continue
		}
		closed = append(closed, target)
	}

	return closed, err
}

func (env *mounter) List() (<-chan multiaddr.Multiaddr, error) {
	var (
		instances = env.hostInstances
		list      = make(chan multiaddr.Multiaddr, len(instances))
	)
	go func() {
		defer close(list)
		for _, instance := range instances {
			list <- instance.Target()
		}
	}()
	return list, nil
}
