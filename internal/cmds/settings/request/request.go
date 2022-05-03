package request

import (
	"context"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// ValueSource uses a cmds.Request as a source for settings values.
func ValueSource(request *cmds.Request) runtime.SetFunc {
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
			assignOrRelay := func(arg runtime.Argument) error {
				var (
					// NOTE: The cmds-libs stores values via the option's primary name.
					// (Meaning we don't need to check keys via aliases.)
					optionName        = arg.Name(parameters.CommandLine)
					cmdsArg, provided = options[optionName]
				)
				if !provided {
					select {
					case unsetArgs <- arg:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return assignOption(arg, cmdsArg, parsers...)
			}
			if err := generic.ForEachOrError(ctx, argsToSet, nil, assignOrRelay); err != nil {
				select {
				case errs <- err:
				case <-ctx.Done():
				}
			}
		}()
		return unsetArgs, errs
	}
}

func assignOption(arg runtime.Argument, cmdsArg any, parsers ...runtime.TypeParser) error {
	switch stringish := cmdsArg.(type) {
	case string:
		parsedArg, err := runtime.ParseStrings(arg, stringish, parsers...)
		if err != nil {
			return err
		}
		return runtime.Assign(arg, parsedArg)
	case []string:
		parsedArg, err := runtime.ParseStrings(arg, stringish, parsers...)
		if err != nil {
			return err
		}
		return runtime.Assign(arg, parsedArg)
	default:
		return runtime.Assign(arg, cmdsArg)
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
