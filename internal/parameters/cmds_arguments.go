package parameters

import (
	"context"
	"fmt"

	cmds "github.com/ipfs/go-ipfs-cmds"
)

// SettingsFromCmds uses a cmds.Request as a source for settings values.
func SettingsFromCmds(request *cmds.Request) SetFunc {
	return func(ctx context.Context,
		argsToSet Arguments, parsers ...TypeParser,
	) (Arguments, <-chan error) {
		options := request.Options
		if !hasUserDefinedOptions(options) {
			// If we have nothing to process,
			// just relay inputs as outputs.
			errs := make(chan error)
			close(errs)
			return argsToSet, errs
		}
		return setEach(ctx, fromRequest(options, parsers...), argsToSet)
	}
}

func fromRequest(options cmds.OptMap, parsers ...TypeParser) providedFunc {
	return func(arg Argument) (provided bool, _ error) {
		var (
			cmdsArg interface{}
			// NOTE: The cmds-libs already stores values
			// into a single map key using the primary name.
			// I.e. we don't need to check each name ourselves.
			commandlineName = arg.Name(CommandLine)
		)
		if cmdsArg, provided = options[commandlineName]; provided {
			if err := assignToArgument(arg, cmdsArg, parsers...); err != nil {
				return false, fmt.Errorf(
					"parameter `%s`: couldn't assign value: %w",
					commandlineName, err)
			}
		}
		return provided, nil
	}
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
