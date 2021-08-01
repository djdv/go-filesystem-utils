package fscmds

import (
	"fmt"
	"strings"
	"sync"

	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs/filesystem/manager"
	"github.com/ipfs/go-ipfs/filesystem/manager/errors"
)

const (
	unmountParameter            = "unmount"
	unmountArgumentDescription  = "Multiaddr style targets to detach from host. " + mountTargetExamples
	unmountAllOptionKwd         = "all"
	unmountAllOptionDescription = "close all active instances (exclusive: do not provide arguments with this flag)"
)

var Unmount = &cmds.Command{
	Options: []cmds.Option{
		cmds.BoolOption(unmountAllOptionKwd, "a", unmountAllOptionDescription),
	},
	Arguments: []cmds.Argument{
		cmds.StringArg(mountStringArgument, false, true, unmountArgumentDescription),
	},
	PreRun: unmountPreRun,
	Run:    unmountRun,
	PostRun: cmds.PostRunMap{
		cmds.CLI: formatUnmount,
	},
	Encoders: cmds.Encoders,
	//Helptext: cmds.HelpText{
	//Tagline:          MountTagline,
	//ShortDescription: mountDescWhatAndWhere,
	//LongDescription:  mountDescWhatAndWhere + "\nExample:\n" + mountDescExample,
	//},
	Type:    manager.Response{},
	NoLocal: true, // always execute on fs service instance
}

// TODO: at least for bool, it looks like something in cmds is catching this before us too
// ^ needs trace to find out where, probably cmds.Run or Execute; if redundant remove all these
// TODO: [general] duplicate arg type checking everywhere 😪
// figure out the best way to abstract this
// we should only have to check them in pre|post(local) + run(remote), not all 3
func unmountPreRun(request *cmds.Request, env cmds.Environment) error {
	closeAll, err := closeAllOption(request)
	if err != nil {
		return err
	}

	if closeAll && len(request.Arguments) != 0 {
		return cmds.Errorf(cmds.ErrClient, "ambiguous request; close-all flag present alongside specific arguments: %s",
			strings.Join(request.Arguments, ", "))
	}

	return nil
}

// --all
func closeAllOption(req *cmds.Request) (closeAll bool, err error) {
	if req == nil {
		return
	}
	if allArg, provided := req.Options[unmountAllOptionKwd]; provided {
		var isBool bool
		if closeAll, isBool = allArg.(bool); !isBool {
			err = cmds.Errorf(cmds.ErrClient,
				"%s's argument %v is type: %T, expecting type: %T",
				unmountAllOptionKwd, allArg, allArg, closeAll)
		}
	}
	return
}

func unmountRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	fsEnv, envIsUsable := env.(FileSystemEnvironment)
	if !envIsUsable {
		return envError(env)
	}

	fsi, err := fsEnv.Index(request)
	if err != nil {
		return err
	}

	var match func(instance manager.Response) bool
	closeAll, err := closeAllOption(request)
	if err != nil {
		return err
	}
	if closeAll {
		match = func(manager.Response) bool { return true }
	} else {
		match = func(instance manager.Response) bool {
			for _, instanceTarget := range request.Arguments {
				if instance.String() == instanceTarget {
					return true
				}
			}
			return false
		}
	}

	var (
		ctx         = request.Context
		inputErrors errors.Stream // intentionally nil, unmount has no possible input errors (yet)
		responses   = fsi.List(ctx)

		wg          sync.WaitGroup
		relay       = make(chan manager.Response, len(responses))
		maybeDetach = func(instance manager.Response) {
			defer wg.Done()
			if match(instance) {
				instance.Error = instance.Close()
				relay <- instance
			}
		}
	)
	go func() {
		defer close(relay)
		for instance := range responses {
			wg.Add(1)
			go maybeDetach(instance)
		}
		wg.Wait()
	}()

	return flattenErrors("unmount", emitResponses(ctx, emitter.Emit,
		inputErrors, relay))
}

func formatUnmount(response cmds.Response, emitter cmds.ResponseEmitter) error {
	var (
		ctx                   = response.Request().Context
		outputs               = makeOptionalOutputs(response.Request(), emitter)
		responses, cmdsErrors = responseToResponses(ctx, response)

		msg = fmt.Sprintf("closing: %s\n", // TODO: different message for closeAll parameter
			strings.Join(response.Request().Arguments, ", "))
		err     = outputs.Print(msg)
		allErrs = renderToConsole(response.Request(), outputs,
			cmdsErrors, responses)
	)
	if err != nil {
		return err
	}
	return flattenErrors("unmount", allErrs)
}
