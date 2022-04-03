package mount

import (
	"context"
	"errors"
	"io"
	goruntime "runtime"
	"strings"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/cmdsenv"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings/runtime"
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

func (self *Settings) Parameters(ctx context.Context) parameters.Parameters {
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
		runtime.GenerateParameters[Settings](ctx, partialParams),
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

type Response struct{ cmdslib.Multiaddr }

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

	mounter := fsEnv.Daemon()
	mountPoints, err := mounter.Mount(request)
	if err != nil {
		return err
	}

	for _, mountPoint := range mountPoints {
		if err := emitter.Emit(&Response{
			Multiaddr: cmdslib.Multiaddr{Interface: mountPoint.Target()},
		}); err != nil {
			return err
		}
	}

	return nil
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
