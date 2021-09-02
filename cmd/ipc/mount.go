package ipc

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/filesystem/cgofuse"
	ipfs "github.com/djdv/go-filesystem-utils/filesystem/ipfscore"
	"github.com/djdv/go-filesystem-utils/filesystem/pinfs"
	cmds "github.com/ipfs/go-ipfs-cmds"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
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
		getIPFSClient = func() (coreiface.CoreAPI, error) {
			ipfsMaddr := settings.IPFSMaddr
			if ipfsMaddr == nil {
				maddr, err := ipfsMaddrFromConfig()
				if err != nil {
					return nil, err
				}
				ipfsMaddr = maddr
			}
			coreAPI := env.ipfsClients.Get(ipfsMaddr)
			if coreAPI == nil {
				core, err := ipfsClient(ipfsMaddr)
				if err != nil {
					return nil, err
				}
				coreAPI = core
				env.ipfsClients.Add(ipfsMaddr, coreAPI)
			}
			return coreAPI, nil
		}
	)

	var (
		binding = binderPair{API: host, ID: fsid}
		mounter = env.mounters.Get(binding)
	)
	if mounter == nil {
		var (
			fileSystem fs.FS
			err        error
		)
		switch fsid {
		case filesystem.IPFS,
			filesystem.IPNS:
			coreAPI, err := getIPFSClient()
			if err != nil {
				return nil, err
			}
			fileSystem = ipfs.NewInterface(env.Context, coreAPI, fsid)
		case filesystem.PinFS:
			coreAPI, err := getIPFSClient()
			if err != nil {
				return nil, err
			}
			fileSystem = pinfs.NewInterface(ctx, coreAPI)
		default:
			return nil, errors.New("TODO: real msg - fsid not supported")
		}

		switch host {
		case filesystem.Fuse:
			mounter, err = cgofuse.NewMounter(env.Context, fileSystem)
		default:
			err = errors.New("TODO: real msg - fsid not supported")
		}
		if err != nil {
			return nil, err
		}

		env.mounters.Add(binding, mounter)
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
		env.instances.Add(targetMaddrs[i], mountPoint)
	}

	return mountPoints, nil
}
