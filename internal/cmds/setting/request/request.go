package request

import (
	"context"

	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/argument"
	"github.com/djdv/go-filesystem-utils/internal/parameter"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// ValueSource uses a cmds.Request as a source for settings values.
func ValueSource(request *cmds.Request) argument.SetFunc {
	return func(ctx context.Context,
		argsToSet argument.Arguments, parsers ...argument.TypeParser,
	) (argument.Arguments, <-chan error) {
		options := request.Options
		if !hasUserDefinedOptions(options) {
			// If we have nothing to process,
			// just relay inputs as outputs.
			errs := make(chan error)
			close(errs)
			return argsToSet, errs
		}
		var (
			unsetArgs = make(chan argument.Argument, cap(argsToSet))
			errs      = make(chan error)
		)
		go func() {
			defer close(unsetArgs)
			defer close(errs)
			for argument := range argsToSet { // TODO: input from public api? needs ctxRange
				var (
					// NOTE: The cmds-libs stores values via the option's primary name.
					// (Meaning we don't need to check keys via aliases.)
					param             = argument.Parameter
					optionName        = param.Name(parameter.CommandLine)
					cmdsArg, provided = options[optionName]
				)
				if !provided {
					select {
					case unsetArgs <- argument:
						continue
					case <-ctx.Done():
						return
					}
				}
				if err := assignOption(argument, cmdsArg, parsers...); err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
					}
					return
				}
			}
		}()
		return unsetArgs, errs
	}
}

func assignOption(arg argument.Argument, cmdsArg any, parsers ...argument.TypeParser) error {
	switch stringish := cmdsArg.(type) {
	case string:
		parsedArg, err := argument.ParseStrings(arg, stringish, parsers...)
		if err != nil {
			return err
		}
		return argument.Assign(arg, parsedArg)
	case []string:
		parsedArg, err := argument.ParseStrings(arg, stringish, parsers...)
		if err != nil {
			return err
		}
		return argument.Assign(arg, parsedArg)
	default:
		return argument.Assign(arg, cmdsArg)
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
