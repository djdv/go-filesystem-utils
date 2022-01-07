package mount

import (
	"errors"
	"io"

	"github.com/djdv/go-filesystem-utils/cmd/environment"
	fscmds "github.com/djdv/go-filesystem-utils/cmd/filesystem"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/filesystem"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

const (
	Name = "mount"

	ArgumentName        = "targets"
	ArgumentDescription = "Multiaddr style targets to bind to host. " + targetExamples
	targetExamples      = "(e.g. `/path/ipfs /path/ipns ...`)" // TODO: platform specific examples
)

var Command = &cmds.Command{
	Arguments: []cmds.Argument{
		cmds.StringArg(ArgumentName, false, true, ArgumentDescription),
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
	Options:  parameters.CmdsOptionsFrom((*Settings)(nil)),
	PostRun: cmds.PostRunMap{
		cmds.CLI: formatMount,
	},
}

type Response struct{ fscmds.Multiaddr }

func mountPreRun(*cmds.Request, cmds.Environment) error {
	return filesystem.RegisterPathMultiaddr()
}

func mountRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	fsEnv, err := environment.Assert(env)
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
			Multiaddr: fscmds.Multiaddr{Interface: mountPoint.Target()},
		}); err != nil {
			return err
		}
	}

	return nil
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
