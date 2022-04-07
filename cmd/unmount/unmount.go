package unmount

import (
	"context"
	"errors"
	"io"

	"github.com/djdv/go-filesystem-utils/cmd/mount"
	"github.com/djdv/go-filesystem-utils/filesystem"
	cmdsenv "github.com/djdv/go-filesystem-utils/internal/cmds/environment"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

const (
	Name = "unmount"
)

type Settings struct {
	All bool
	mount.Settings
	settings.Root
}

func (self *Settings) Parameters(ctx context.Context) parameters.Parameters {
	partialParams := []runtime.CmdsParameter{
		{
			OptionAliases: []string{"a"},
			HelpText:      "Unmount all mountpoints.",
		},
	}
	return CtxMerge(ctx,
		runtime.GenerateParameters[Settings](ctx, partialParams),
		(*mount.Settings)(nil).Parameters(ctx),
		(*settings.Root)(nil).Parameters(ctx),
	)
}

func Command() *cmds.Command {
	return &cmds.Command{
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
		Options:  settings.MakeOptions[Settings](),
		PostRun: cmds.PostRunMap{
			cmds.CLI: formatUnmount,
		},
	}
}

type Response struct{ multiaddr.Multiaddr }

func unmountPreRun(*cmds.Request, cmds.Environment) error {
	return filesystem.RegisterPathMultiaddr()
}

func unmountRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	// TODO: [continuity;IPC]
	// We don't parse the request ourselves; we just pass it to the IPC server.
	// Environment variables on the client are not going to exist there.
	// This might be confusing.
	// We should parse the request ourselves and pass arguments to ipc.Unmount
	//
	// This requires the environment interface to change / stabilise.
	// Right now it's not clear what it should look like. But probably something like
	// `Unmount(targets <-chan maddr, opts options...) mounted <-chan maddrs, errors <-chan error
	// This is how the prototype did it and it made the most sense.
	// It's a closer approximation to what's really happening (sequence of HTTP requests+response)
	// and it's real-time rather than atomic.
	// (results come back ASAP, as opposed to returning only after each one has been processed in bulk)

	fsEnv, err := cmdsenv.Assert(env)
	if err != nil {
		return err
	}

	ctx := request.Context
	unmounter := fsEnv.Daemon().Mounter()
	formerTargets, errs, err := unmounter.Unmount(ctx, 0, 0, nil) // FIXME:
	if err != nil {
		return err
	}

	fn := func(mountpoint filesystem.MountPoint) error {
		return emitter.Emit(&Response{Multiaddr: mountpoint.Target()})
	}
	return generic.ForEachOrError(ctx, formerTargets, errs, fn)
}

/*
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
		outputs.Print(response.Multiaddr.String() + "\n")
		outputs.Emit(response)
	}
}
*/

func formatUnmount(response cmds.Response, emitter cmds.ResponseEmitter) error {
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
