package list

import (
	"errors"
	"io"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/cmdsenv"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

const Name = "list"

var Command = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "List file systems bound to the host.",
	},
	NoLocal:  true, // Always communicate with the file system service (as a client).
	Encoders: settings.CmdsEncoders,
	Type:     Response{},
	PreRun:   listPreRun,
	Run:      listRun,
	PostRun: cmds.PostRunMap{
		cmds.CLI: formatList,
	},
}

type Response struct{ cmdslib.Multiaddr }

func listPreRun(*cmds.Request, cmds.Environment) error {
	return filesystem.RegisterPathMultiaddr()
}

func listRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	serviceEnv, err := cmdsenv.Assert(env)
	if err != nil {
		return err
	}

	lister := serviceEnv.Daemon()
	// mountPoints, err := lister.List(request)
	// TODO: cmds request -> list options
	mountPoints, err := lister.List()
	if err != nil {
		return err
	}

	for mountPoint := range mountPoints {
		if err := emitter.Emit(&Response{
			Multiaddr: cmdslib.Multiaddr{Interface: mountPoint},
		}); err != nil {
			return err
		}
	}

	return nil
}

func formatList(response cmds.Response, emitter cmds.ResponseEmitter) error {
	var gotResponse bool
out:
	for {
		untypedResponse, err := response.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
			break out
		}

		response, ok := untypedResponse.(*Response)
		if !ok {
			return cmds.Errorf(cmds.ErrImplementation,
				"emitter sent unexpected type+value: %#v", untypedResponse)
		}
		gotResponse = true

		// TODO: Format into table.
		emitter.Emit(response.Multiaddr.String())
	}

	if !gotResponse {
		if err := emitter.Emit("No active instances"); err != nil {
			return err
		}
	}

	return nil
}

/*
func formatList(response cmds.Response, emitter cmds.ResponseEmitter) error {
	var (
		gotResponse bool
		outputs     = formats.MakeOptionalOutputs(response.Request(), emitter)
	)
out:
	for {
		untypedResponse, err := response.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
			break out
		}

		response, ok := untypedResponse.(*Response)
		if !ok {
			return cmds.Errorf(cmds.ErrImplementation,
				"emitter sent unexpected type+value: %#v", untypedResponse)
		}
		gotResponse = true

		// TODO: Format into table.
		outputs.Print(response.Multiaddr.String() + "\n")
		outputs.Emit(response)
	}

	if !gotResponse {
		if err := outputs.Print("No active instances\n"); err != nil {
			return err
		}
	}

	return nil
}
*/
