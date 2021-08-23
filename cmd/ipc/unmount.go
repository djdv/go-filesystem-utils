package ipc

import (
	"fmt"

	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

// TODO: channel outputs
func (env *daemonEnvironment) Unmount(request *cmds.Request) ([]multiaddr.Multiaddr, error) {
	var (
		ctx             = request.Context
		settings        = new(UnmountSettings)
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

	// TODO: parse these properly (use parameters lib)
	// quick hacks for now
	var all bool
	allVal, ok := request.Options[All().CommandLine()]
	if ok {
		all, ok = allVal.(bool)
	}

	var (
		closed = make([]multiaddr.Multiaddr, 0, len(targetMaddrs))
		err    error
	)
	if all {
		// TODO: alloc once
		closed = make([]multiaddr.Multiaddr, 0, len(env.instances))
		// TODO: [port] make sure to prevent calling --all with args too
		for _, mountPoint := range env.instances {
			target := mountPoint.Target()
			if cErr := env.instances.Close(target); cErr != nil {
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
		if cErr := env.instances.Close(target); cErr != nil {
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
