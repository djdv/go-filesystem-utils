package list

import (
	"errors"
	"io"

	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	"github.com/djdv/go-filesystem-utils/filesystem"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

const Name = "list"

var Command = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "List file systems bound to the host.",
	},
	NoLocal:  true, // Always communicate with the file system service (as a client).
	Encoders: formats.CmdsEncoders,
	Type:     Response{},
	PreRun:   listPreRun,
	Run:      listRun,
	PostRun: cmds.PostRunMap{
		cmds.CLI: formatList,
	},
}

type Response struct{ formats.Multiaddr }

func listPreRun(*cmds.Request, cmds.Environment) error {
	return filesystem.RegisterPathMultiaddr()
}

func listRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	fsEnv, err := ipc.CastEnvironment(env)
	if err != nil {
		return err
	}

	mountPoints, err := fsEnv.List(request)
	if err != nil {
		return err
	}

	for _, mountPoint := range mountPoints {
		if err := emitter.Emit(&Response{
			Multiaddr: formats.Multiaddr{Interface: mountPoint},
		}); err != nil {
			return err
		}
	}

	return nil
}

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
		outputs.Print(response.Multiaddr.String())
		outputs.Emit(response)
	}

	if !gotResponse {
		if err := outputs.Print("No active instances\n"); err != nil {
			return err
		}
	}

	return nil
}
