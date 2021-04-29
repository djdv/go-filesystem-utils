package list

import (
	"fmt"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/formats"
	"github.com/djdv/go-filesystem-utils/manager"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

const (
	Name        = "list"
	description = "List active file system instances"
)

var Command = &cmds.Command{
	NoLocal: true,
	Run:     listRun,
	PostRun: cmds.PostRunMap{
		cmds.CLI: formatList,
	},
	Encoders: manager.ResponseEncoderMap,
	Helptext: cmds.HelpText{
		Tagline: description,
	},
	Type: manager.Response{},
}

func listRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	fsEnv, err := fscmds.AssertEnvironment(env)
	if err != nil {
		return err
	}

	ctx := request.Context

	return flattenErrors("listing",
		formats.EmitResponses(ctx, emitter.Emit,
			fsEnv.Index().List(ctx),
		),
	)
}

func formatList(response cmds.Response, emitter cmds.ResponseEmitter) error {
	var (
		ctx                   = response.Request().Context
		responses, cmdsErrors = formats.CmdsResponseToManagerResponses(ctx, response)
		responseRelay         = make(chan manager.Response, len(responses))
		gotResponse           bool

		outputs      = formats.MakeOptionalOutputs(response.Request(), emitter)
		renderErrors = formats.RenderResponseToOutputs(ctx, responseRelay, outputs)
	)

	go func() {
		defer close(responseRelay)
		for {
			select {
			case response, ok := <-responses:
				if !ok {
					return
				}
				gotResponse = true
				responseRelay <- response
			case <-ctx.Done():
				return
			}
		}
	}()

	if err := <-cmdsErrors; err != nil {
		return err
	}

	renderError := flattenErrors("render", renderErrors)
	if !gotResponse && renderError == nil {
		err := outputs.Print("No active instances\n")
		if err != nil {
			return err
		}
	}

	return renderError
}

func flattenErrors(prefix string, errs <-chan error) (err error) {
	for chanErr := range errs {
		if err == nil {
			err = fmt.Errorf("%s: %w", prefix, chanErr)
			continue
		}
		err = fmt.Errorf("%w, %s", err, chanErr)
	}
	return
}
