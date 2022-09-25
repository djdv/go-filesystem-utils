package commands

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/daemon"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	giconfig "github.com/ipfs/kubo/config"
	"github.com/multiformats/go-multiaddr"
)

type (
	mountSettings     struct{ commonSettings }
	mountFuseSettings struct{ commonSettings }
	mountIPFSSettings struct {
		ipfsAPI multiaddr.Multiaddr
		clientSettings
	}
)

// TODO: move; should be in shared or even in [command] pkg.
func subonlyExec[settings command.Settings[T], cmd command.ExecuteFuncArgs[settings, T], T any]() cmd {
	return func(context.Context, settings, ...string) error {
		// This command only holds subcommands
		// and has no functionality on its own.
		return command.ErrUsage
	}
}

func (set *mountSettings) BindFlags(fs *flag.FlagSet) {
	set.commonSettings.BindFlags(fs)
}

func (set *mountFuseSettings) BindFlags(fs *flag.FlagSet) {
	set.commonSettings.BindFlags(fs)
}

func (set *mountIPFSSettings) BindFlags(fs *flag.FlagSet) {
	set.clientSettings.BindFlags(fs)
	const apiFile = "api"
	var (
		envAPI  = filepath.Join(giconfig.EnvDir, apiFile)
		fileAPI = filepath.Join(giconfig.DefaultPathRoot, apiFile)
	)
	// TODO: this should be a string, not parsed client-side
	// (server may have different namespaces registered + double parse;
	// just passthrough argv[x] as-is)
	// TODO: default string option. Currently mocked.
	multiaddrVar(fs, &set.ipfsAPI, "ipfs",
		nil, fmt.Sprintf(
			"IPFS API node `maddr`. (default: \"$%s\", \"%s\")",
			envAPI, fileAPI,
		))
}

func Mount() command.Command {
	const (
		name     = "mount"
		synopsis = "Mount a file system."
		usage    = "Placeholder text."
	)
	return command.MakeCommand[*mountSettings](name, synopsis, usage,
		subonlyExec[*mountSettings](),
		command.WithSubcommands(makeMountSubcommands()...),
	)
}

func mountFuse() command.Command {
	const usage = "Placeholder text."
	var (
		formalName = filesystem.Fuse.String()
		cmdName    = strings.ToLower(formalName)
		synopsis   = fmt.Sprintf("Mount a file system via the %s API.", formalName)
	)
	return command.MakeCommand[*mountFuseSettings](cmdName, synopsis, usage,
		subonlyExec[*mountFuseSettings](),
		command.WithSubcommands(makeMountFuseSubcommands()...),
	)
}

func makeMountSubcommands() []command.Command {
	var (
		hostAPIs = []filesystem.API{
			filesystem.Fuse,
			// TODO: ...
		}
		subcommands = make([]command.Command, len(hostAPIs))
	)
	for i, hostAPI := range hostAPIs {
		switch hostAPI {
		case filesystem.Fuse:
			subcommands[i] = mountFuse()
		default:
			panic("unexpected API ID for host file system interface")
		}
	}
	return subcommands
}

func makeMountFuseSubcommands() []command.Command {
	const usage = "Placeholder text."
	var (
		formalName = filesystem.Fuse.String()
		targetAPIs = []filesystem.ID{
			filesystem.IPFS,
			filesystem.IPFSPins,
			filesystem.IPNS,
			filesystem.IPFSKeys,
			// TODO: ...
		}
		subcommands = make([]command.Command, len(targetAPIs))
	)
	for i, fsid := range targetAPIs {
		var (
			fsName     = fsid.String()
			subcmdName = strings.ToLower(fsName)
			synopsis   = fmt.Sprintf("Mount %s via the %s API.", fsName, formalName)
		)
		switch fsid {
		case filesystem.IPFS, filesystem.IPFSPins,
			filesystem.IPNS, filesystem.IPFSKeys:
			subcommands[i] = command.MakeCommand[*mountIPFSSettings](subcmdName, synopsis, usage,
				makeFuseIPFSExec(filesystem.Fuse, fsid),
			)
		default:
			panic("unexpected API ID for host file system interface")
		}
	}
	return subcommands
}

func makeFuseIPFSExec(host filesystem.API, fsid filesystem.ID) func(context.Context, *mountIPFSSettings, ...string) error {
	return func(ctx context.Context, set *mountIPFSSettings, args ...string) error {
		return ipfsExecute(ctx, host, fsid, set, args...)
	}
}

func ipfsExecute(ctx context.Context, host filesystem.API, fsid filesystem.ID,
	set *mountIPFSSettings, args ...string,
) error {
	var (
		err          error
		serviceMaddr = set.serviceMaddr

		client     *daemon.Client
		clientOpts []daemon.ClientOption

		// TODO: quick hack; do better
		defaultMaddrs bool
		//
	)
	// TODO: [31f421d5-cb4c-464e-9d0f-41963d0956d1]
	if lazy, ok := serviceMaddr.(lazyFlag[multiaddr.Multiaddr]); ok {
		serviceMaddr = lazy.get()
		defaultMaddrs = true
	}
	if set.verbose {
		// TODO: less fancy prefix and/or out+prefix from CLI flags
		clientLog := log.New(os.Stdout, "⬇️ client - ", log.Lshortfile)
		clientOpts = append(clientOpts, daemon.WithLogger(clientLog))
	}
	if defaultMaddrs {
		client, err = daemon.ConnectOrLaunchLocal(clientOpts...)
	} else {
		client, err = daemon.Connect(serviceMaddr, clientOpts...)
	}
	if err != nil {
		return err
	}

	var mountOpts []daemon.MountOption
	if ipfsMaddr := set.ipfsAPI; ipfsMaddr != nil {
		mountOpts = append(mountOpts, daemon.WithIPFS(ipfsMaddr))
	}
	if err := client.Mount(host, fsid, args, mountOpts...); err != nil {
		return err
	}
	if err := client.Close(); err != nil {
		return err
	}

	return ctx.Err()
}
