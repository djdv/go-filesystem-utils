package fscmds

import (
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs/filesystem/manager"
	"github.com/ipfs/go-ipfs/filesystem/manager/errors"
)

const (
	listParameter   = "list"
	listDescription = "list active instances"
)

var List = &cmds.Command{
	Run: listRun,
	PostRun: cmds.PostRunMap{
		cmds.CLI: formatList,
	},
	Encoders: cmds.Encoders,
	Helptext: cmds.HelpText{ // TODO: docs are still outdated - needs sys_ migrations
		Tagline:          "TODO: tagline",
		ShortDescription: listDescription,
		LongDescription:  listDescription,
	},
	Type:    manager.Response{},
	NoLocal: true, // always execute on fs service instance
}

func listRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	fsEnv, envIsUsable := env.(FileSystemEnvironment)
	if !envIsUsable {
		return envError(env)
	}

	fsi, err := fsEnv.Index(request)
	if err != nil {
		return cmds.Errorf(cmds.ErrImplementation, err.Error())
	}

	var (
		ctx         = request.Context
		inputErrors errors.Stream // intentionally nil, list has no possible input errors (yet)
		responses   = fsi.List(ctx)
		allErrs     = emitResponses(ctx, emitter.Emit,
			inputErrors, responses)
	)

	return flattenErrors("listing", allErrs) // TODO: pull name prefix from request path
}

func formatList(response cmds.Response, emitter cmds.ResponseEmitter) (err error) {
	var (
		ctx                   = response.Request().Context
		outputs               = makeOptionalOutputs(response.Request(), emitter)
		responses, cmdsErrors = responseToResponses(ctx, response)

		gotResponse bool
		relay       = make(chan manager.Response, len(responses))
	)
	go func() {
		defer close(relay)
		for {
			select {
			case response, ok := <-responses:
				if !ok {
					return
				}
				gotResponse = true
				relay <- response
			case <-ctx.Done():
				return
			}
		}
	}()
	err = flattenErrors("listing", renderToConsole(response.Request(), outputs,
		cmdsErrors, relay))
	if !gotResponse && err == nil {
		if err = outputs.Print("No active instances\n"); err != nil {
			return
		}
	}
	return
}
