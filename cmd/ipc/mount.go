package ipc

import (
	"fmt"
	"io/fs"

	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/filesystem"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

func (env *daemonEnvironment) Mount(request *cmds.Request) ([]filesystem.MountPoint, error) {
	var (
		ctx             = env.Context
		settings        = new(MountSettings)
		unsetArgs, errs = parameters.ParseSettings(ctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
	)
	if _, err := parameters.AccumulateArgs(ctx, unsetArgs, errs); err != nil {
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
		host = settings.HostAPI
		fsid = settings.FSID
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
			ipfsMaddr = settings.IPFSMaddr
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
