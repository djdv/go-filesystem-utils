package arguments

import (
	"context"
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// SettingsFromCmds uses a cmds.Request as a source for settings values.
func SettingsFromCmds(request *cmds.Request) runtime.SetFunc {
	return func(ctx context.Context,
		argsToSet runtime.Arguments, parsers ...runtime.TypeParser,
	) (runtime.Arguments, <-chan error) {
		options := request.Options
		if !hasUserDefinedOptions(options) {
			// If we have nothing to process,
			// just relay inputs as outputs.
			errs := make(chan error)
			close(errs)
			return argsToSet, errs
		}

		var (
			unsetArgs = make(chan runtime.Argument, cap(argsToSet))
			errs      = make(chan error)
		)
		go func() {
			defer close(unsetArgs)
			defer close(errs)
			fn := func(unsetArg runtime.Argument) (runtime.Argument, error) {
				provided, err := fromRequest(unsetArg, options, parsers...)
				if err != nil {
					return unsetArg, err
				}
				if provided { // We're going to process this, skip relaying it.
					return unsetArg, ErrSkip
				}
				return unsetArg, nil
			}

			ProcessResults(ctx, argsToSet, unsetArgs, errs, fn)
		}()
		return unsetArgs, errs
	}
}

func fromRequest(arg runtime.Argument, options cmds.OptMap, parsers ...runtime.TypeParser) (provided bool, _ error) {
	var (
		cmdsArg interface{}
		// NOTE: The cmds-libs already stores values
		// into a single map key using the primary name.
		// I.e. we don't need to check each name ourselves.
		commandlineName = arg.Name(parameters.CommandLine)
	)
	if cmdsArg, provided = options[commandlineName]; provided {
		// TODO: [maybe] type check `cmdsArg` and pass to runtime.Parse if string?
		// ^ We really just need a better name for AssignToArgument, that does maybeParse and Assign
		// with the assignment part split out.
		if err := runtime.ParseAndAssign(arg, cmdsArg, parsers...); err != nil {
			return false, fmt.Errorf(
				"parameter `%s`: couldn't assign value: %w",
				commandlineName, err)
		}
	}
	return provided, nil
}

func hasUserDefinedOptions(options cmds.OptMap) bool {
	var (
		hasUserOptions bool
		cmdsExclusive  = [...]string{
			cmds.EncLong,
			cmds.RecLong,
			cmds.ChanOpt,
			cmds.TimeoutOpt,
			cmds.DerefLong,
			cmds.StdinName,
			cmds.Hidden,
			cmds.Ignore,
			cmds.IgnoreRules,
			cmds.OptLongHelp,
			cmds.OptShortHelp,
		}
	)
optChecker:
	for optName := range options {
		for _, cmdsName := range cmdsExclusive {
			if optName == cmdsName {
				continue optChecker
			}
		}
		hasUserOptions = true
		break
	}
	return hasUserOptions
}
