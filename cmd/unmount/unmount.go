package unmount

import (
	"errors"
	"io"

	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	"github.com/djdv/go-filesystem-utils/cmd/mount"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/filesystem"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

const (
	Name = "unmount"
)

// TODO: extend mount settings; --all

var Command = &cmds.Command{
	Arguments: []cmds.Argument{
		cmds.StringArg(mount.ArgumentName, false, true, mount.ArgumentDescription),
		// TODO: stdin handling
	},
	Helptext: cmds.HelpText{
		Tagline: "Detach file systems from the host.",
	},
	NoLocal:  true, // Always communicate with the file system service (as a client).
	Encoders: cmds.Encoders,
	Type:     Response{},
	PreRun:   unmountPreRun,
	Run:      unmountRun,
	Options:  parameters.CmdsOptionsFrom((*ipc.UnmountSettings)(nil)),
	PostRun: cmds.PostRunMap{
		cmds.CLI: formatUnmount,
	},
}

type Response struct{ formats.Multiaddr }

func unmountPreRun(*cmds.Request, cmds.Environment) error {
	return filesystem.RegisterPathMultiaddr()
}

func unmountRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	var (
		ctx             = request.Context
		settings        = new(mount.Settings)
		unsetArgs, errs = parameters.ParseSettings(ctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
	)
	if _, err := parameters.AccumulateArgs(ctx, unsetArgs, errs); err != nil {
		return err
	}

	fsEnv, err := ipc.CastEnvironment(env)
	if err != nil {
		return err
	}
	formerTargets, err := fsEnv.Unmount(request)
	if err != nil {
		return err
	}

	for _, target := range formerTargets {
		if err := emitter.Emit(&Response{
			Multiaddr: formats.Multiaddr{Interface: target},
		}); err != nil {
			return err
		}
	}

	return nil
}
func formatUnmount(response cmds.Response, emitter cmds.ResponseEmitter) error {
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
		outputs.Print(response.Multiaddr.String())
		outputs.Emit(response)
	}
}
