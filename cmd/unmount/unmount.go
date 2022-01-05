package unmount

import (
	"errors"
	"io"

	serviceenv "github.com/djdv/go-filesystem-utils/cmd/environment"
	fscmds "github.com/djdv/go-filesystem-utils/cmd/filesystem"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
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
	Options:  parameters.CmdsOptionsFrom((*fscmds.UnmountSettings)(nil)),
	PostRun: cmds.PostRunMap{
		cmds.CLI: formatUnmount,
	},
}

type Response struct{ formats.Multiaddr }

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

	fsEnv, err := serviceenv.Assert(env)
	if err != nil {
		return err
	}

	unmounter := fsEnv.Daemon().Mounter()
	formerTargets, err := unmounter.Unmount(request)
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
