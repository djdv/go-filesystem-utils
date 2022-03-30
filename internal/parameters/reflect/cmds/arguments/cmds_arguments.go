package arguments

import (
	"context"
	"fmt"

	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	"github.com/djdv/go-filesystem-utils/internal/parameters/reflect"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// SettingsFromCmds uses a cmds.Request as a source for settings values.
func SettingsFromCmds(request *cmds.Request) SetFunc {
	return func(ctx context.Context,
		argsToSet reflect.Arguments, parsers ...reflect.TypeParser,
	) (reflect.Arguments, <-chan error) {
		options := request.Options
		if !hasUserDefinedOptions(options) {
			// If we have nothing to process,
			// just relay inputs as outputs.
			errs := make(chan error)
			close(errs)
			return argsToSet, errs
		}

		var (
			unsetArgs = make(chan reflect.Argument, cap(argsToSet))
			errs      = make(chan error)
		)
		go func() {
			defer close(unsetArgs)
			defer close(errs)
			fn := func(unsetArg reflect.Argument) (reflect.Argument, error) {
				provided, err := fromRequest(unsetArg, options, parsers...)
				if err != nil {
					return unsetArg, err
				}
				if provided {
					return unsetArg, ErrSkip
				}
				return unsetArg, nil
			}

			ProcessResults(ctx, argsToSet, unsetArgs, errs, fn)
		}()
		return unsetArgs, errs
	}
}

func fromRequest(arg reflect.Argument, options cmds.OptMap, parsers ...reflect.TypeParser) (provided bool, _ error) {
	var (
		cmdsArg interface{}
		// NOTE: The cmds-libs already stores values
		// into a single map key using the primary name.
		// I.e. we don't need to check each name ourselves.
		commandlineName = arg.Name(parameters.CommandLine)
	)
	if cmdsArg, provided = options[commandlineName]; provided {
		if err := reflect.AssignToArgument(arg, cmdsArg, parsers...); err != nil {
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
