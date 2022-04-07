package mount

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/filesystem/cgofuse"
	ipfsconfig "github.com/ipfs/go-ipfs-config"
	ipfsconfigfile "github.com/ipfs/go-ipfs-config/serialize"
	"github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
)

type (
	Mounter interface {
		Mount(ctx context.Context,
			hostAPI filesystem.API, fsID filesystem.ID,
			targets []multiaddr.Multiaddr,
			opts ...Option) (<-chan filesystem.MountPoint, <-chan error, error)
		Unmount(ctx context.Context,
			hostAPI filesystem.API, fsID filesystem.ID,
			targets []multiaddr.Multiaddr,
			opts ...Option) (<-chan filesystem.MountPoint, <-chan error, error)
		// TODO: options parameter: ...opts
		// parse: cmds.Request's `-a` => list(listOptAll(true))
		// TODO: List should take a context.
		List(ctx context.Context) (<-chan multiaddr.Multiaddr, error)
	}

	binderPair struct {
		identifier string
		fsid       filesystem.ID
	}
	binderMap map[binderPair]filesystem.Mounter

	instanceMap map[string]filesystem.MountPoint
	fsidMap     map[filesystem.ID]fs.FS

	mounter struct {
		context.Context
		// FIXME: ^ this needs annotations
		// it's the context passed to MakeEnv i.e. the daemon's context
		// it needs to be respected WITH the request ctx
		// If canceled while client request is in progress, we should wrap the error
		// "environment: context canceled" to signify the server died
		hostBinders   binderMap
		hostInstances instanceMap

		ipfsBindings ipfsMap
	}
)

func New(ctx context.Context) *mounter {
	return &mounter{Context: ctx}
}

func (env *mounter) Mount(ctx context.Context,
	hostAPI filesystem.API, fsID filesystem.ID,
	targets []multiaddr.Multiaddr, opts ...Option) (<-chan filesystem.MountPoint, <-chan error, error) {
	var (
		settings = parseOptions(opts...)
		// TODO: Reconsider how to distinguish sets.
		// We need some kind interface for this.
		// myfs.uuid(), myfs.hashfn(somethingUnique), etc.
		// Anything to split up things like IPFS targets used
		// with the host API; but generic, not strictly a maddr.
		fileSystem fs.FS
		identifier string
	)
	switch fsID {
	case filesystem.IPFS,
		filesystem.IPNS,
		filesystem.PinFS,
		filesystem.KeyFS:
		if settings.ipfsAPI == nil {
			ipfsMaddr, err := ipfsMaddrFromConfig()
			if err != nil {
				return nil, nil, err
			}
			settings.ipfsAPI = ipfsMaddr

			identifier = ipfsMaddr.String() // delineate via the node maddr
			if fileSystem, err = env.getIPFS(fsID, ipfsMaddr); err != nil {
				return nil, nil, err
			}
		}
	default:
		err := fmt.Errorf("TODO: real msg - fsid \"%s\" not yet supported", fsID)
		return nil, nil, err
	}

	var (
		out  = make(chan filesystem.MountPoint, len(targets))
		errs = make(chan error)
	)
	go func() {
		defer close(out)
		defer close(errs)
		var (
			ctx             = env.Context
			mounter         filesystem.Mounter
			mountIdentifier = binderPair{fsid: fsID, identifier: identifier}
		)
		// FIXME: quick port hack
		// we need to properly refactor this whole chain of functions
		switch hostAPI {
		case filesystem.Fuse:
			var err error
			if mounter, err = env.getFuse(mountIdentifier, fileSystem); err != nil {
				//return nil, err
				errs <- err
				return
			}
		default:
			//return nil, fmt.Errorf("TODO: real msg - host API \"%s\" not yet supported", host)
			err := fmt.Errorf("TODO: real msg - host API \"%s\" not yet supported", hostAPI)
			errs <- err
			return
		}

		mountPoints := make([]filesystem.MountPoint, 0, len(targets))
		for _, target := range targets {
			mountPoint, err := mounter.Mount(env.Context, target)
			if err != nil {
				for _, mountPoint := range mountPoints {
					if unmountErr := mountPoint.Close(); unmountErr != nil {
						err = fmt.Errorf("%w - %s", err, unmountErr)
					}
				}
				select {
				case errs <- err:
				case <-ctx.Done():
				}
				return
			}
			select {
			case out <- mountPoint:
			case <-ctx.Done():
				return
			}
		}

		// Only store these after success mounting above.
		for i, mountPoint := range mountPoints {
			env.hostInstances.Add(targets[i], mountPoint)
		}
	}()
	return out, errs, nil
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

func (env *mounter) Unmount(ctx context.Context,
	HostAPI filesystem.API, fsID filesystem.ID,
	targets []multiaddr.Multiaddr, opts ...Option) (<-chan filesystem.MountPoint, <-chan error, error) {

	// TODO: validate inputs first?
	var (
		out = make(chan filesystem.MountPoint, len(targets))
		//errs = make(chan error)
		errs = make(chan error, 1)
	)
	go func() {
		defer close(out)
		defer close(errs)
		errs <- errors.New("not ported yet")
	}()
	return out, errs, nil

	/*
		// TODO: convert to options
		unmountSettings, err := settings.Parse[settings.UnmountSettings](ctx, request)
		if err != nil {
			return nil, err
		}

		var (
			// TODO: caller needs to pass to us.
			// Mutually exclusive with `-a` (args and no -a or nil and -a)
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
	*/
}

func (env *mounter) List(ctx context.Context) (<-chan multiaddr.Multiaddr, error) {
	var (
		instances = env.hostInstances
		list      = make(chan multiaddr.Multiaddr, len(instances))
	)
	go func() {
		defer close(list)
		for _, instance := range instances {
			select {
			case list <- instance.Target():
			case <-ctx.Done():
				return
			}
		}
	}()
	return list, nil
}
