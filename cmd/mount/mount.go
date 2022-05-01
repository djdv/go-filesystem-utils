package mount

import (
	"context"
	"errors"
	"fmt"
	"io"
	goruntime "runtime"
	"strings"

	"github.com/djdv/go-filesystem-utils/filesystem"
	cmdsenv "github.com/djdv/go-filesystem-utils/internal/cmds/environment"
	"github.com/djdv/go-filesystem-utils/internal/cmds/environment/mount"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

const (
	Name = "mount"

	ArgumentName        = "targets"
	ArgumentDescription = "Multiaddr style targets to bind to host."

	hostAPIParam      = "system"
	fileSystemIDParam = "fs"
)

type Settings struct {
	HostAPI   filesystem.API
	FSID      filesystem.ID
	IPFSMaddr multiaddr.Multiaddr
	settings.Root
}

func (*Settings) Parameters(ctx context.Context) parameters.Parameters {
	partialParams := []runtime.CmdsParameter{
		{
			OptionName: hostAPIParam,
			HelpText:   "Host system API to use.",
		},
		{
			OptionName: fileSystemIDParam,
			HelpText:   "Target FS to use.",
		},
		{
			OptionName: "ipfs",
			HelpText:   "IPFS multiaddr to use.",
		},
	}
	return CtxJoin(ctx,
		runtime.MustMakeParameters[*Settings](ctx, partialParams),
		(*settings.Root).Parameters(nil, ctx),
	)
}

func Command() *cmds.Command {
	Command := &cmds.Command{
		Arguments: []cmds.Argument{
			cmds.StringArg(ArgumentName, false, true,
				ArgumentDescription+" "+descriptionString(true, examplePaths()),
			),
			// TODO: stdin handling
		},
		Helptext: cmds.HelpText{
			Tagline: "Bind file systems to the host.",
		},
		NoLocal:  true, // Always communicate with the file system service (as a client).
		Encoders: cmds.Encoders,
		Type:     Response{},
		PreRun:   mountPreRun,
		Run:      mountRun,
		Options:  settings.MakeOptions[Settings](),
		PostRun: cmds.PostRunMap{
			cmds.CLI: formatMount,
		},
	}
	registerMountSubcommands(Command)
	return Command
}

type Response struct{ multiaddr.Multiaddr }

func mountPreRun(request *cmds.Request, _ cmds.Environment) error {
	if err := checkSubCmd(2, request.Path); err != nil {
		return err
	}
	return filesystem.RegisterPathMultiaddr()
}

func mountRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	fsEnv, err := cmdsenv.Assert(env)
	if err != nil {
		return err
	}

	ctx := request.Context
	settings, err := settings.Parse[*Settings](ctx, request)
	if err != nil {
		return err
	}
	argMaddrs, err := parseArgs(request.Arguments)
	var (
		opts    []mount.Option
		mounter = fsEnv.Daemon().Mounter()
		host    = settings.HostAPI
		fsid    = settings.FSID
		ipfs    = settings.IPFSMaddr // TODO: optional
	)
	// TODO: use dynamic default values
	// (the one most appropriate for the current system)
	if host == 0 {
		host = filesystem.Fuse
	}
	if fsid == 0 {
		fsid = filesystem.IPFS
	}
	if ipfs != nil {
		opts = append(opts, mount.WithIPFS(ipfs))
	}

	mountPoints, errs, err := mounter.Mount(ctx, host, fsid, argMaddrs, opts...)
	if err != nil {
		return err
	}

	// TODO: less closure
	var (
		mountErr error
		addErr   = func(e error) {
			if mountErr == nil {
				mountErr = e
			} else {
				mountErr = fmt.Errorf("%w - %s", mountErr, e)
			}
		}
		cache  = make([]filesystem.MountPoint, 0, cap(mountPoints))
		unwind = func() {
			for _, mountPoint := range cache {
				if unmountErr := mountPoint.Close(); unmountErr != nil {
					addErr(unmountErr)
				}
			}
		}
	)
	for mountPoints != nil ||
		errs != nil {
		select {
		case mountPoint, ok := <-mountPoints:
			if !ok {
				mountPoints = nil
				continue
			}
			cache = append(cache, mountPoint)
			if err := emitter.Emit(&Response{
				//Multiaddr: cmdslib.Multiaddr{Interface: mountPoint.Target()},
				Multiaddr: mountPoint.Target(),
			}); err != nil {
				addErr(err)
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			addErr(err)
		}
	}

	if mountErr != nil {
		unwind()
	}
	return mountErr
}

func parseArgs(args []string) ([]multiaddr.Multiaddr, error) {
	targetMaddrs := make([]multiaddr.Multiaddr, len(args))
	for i, target := range args {
		maddr, err := multiaddr.NewMultiaddr(target)
		if err != nil {
			return nil, err
		}
		targetMaddrs[i] = maddr
	}
	return targetMaddrs, nil
}

func examplePaths() []string {
	// TODO: build constraints
	if goruntime.GOOS == "windows" {
		return []string{
			`I:`,
			`C:\ipfs`,
			`\\localhost\ipfs`,
		}
	}
	return []string{
		`/mnt/ipfs`,
		`/mnt/ipns`,
	}
}

func descriptionString(canonical bool, paths []string) string {
	var builder strings.Builder
	builder.WriteString("(E.g. `")

	for _, path := range paths {
		if canonical {
			builder.WriteString("/path")
			// TODO: build constraints
			if goruntime.GOOS == "windows" {
				builder.WriteRune('/')
			}
		}
		builder.WriteString(path)
		builder.WriteRune(' ')
	}

	builder.WriteString("...`)")
	return builder.String()
}

/*
func formatMount(response cmds.Response, emitter cmds.ResponseEmitter) error {
	outputs := formats.MakeOptionalOutputs(response.Request(), emitter)
	for {
		untypedResponse, err := response.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
			return nil
		}

		response, ok := untypedResponse.(*Response)
		if !ok {
			return cmds.Errorf(cmds.ErrImplementation,
				"emitter sent unexpected type+value: %#v", untypedResponse)
		}

		// TODO: Format into table.
		outputs.Print(response.Multiaddr.String() + "\n")
		outputs.Emit(response)
	}
}
*/

func formatMount(response cmds.Response, emitter cmds.ResponseEmitter) error {
	for {
		untypedResponse, err := response.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
			return nil
		}

		response, ok := untypedResponse.(*Response)
		if !ok {
			return cmds.Errorf(cmds.ErrImplementation,
				"emitter sent unexpected type+value: %#v", untypedResponse)
		}

		// TODO: Format into table.
		emitter.Emit(response.Multiaddr.String() + "\n")
	}
}
