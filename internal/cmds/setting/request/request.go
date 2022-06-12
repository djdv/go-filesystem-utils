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
		argsToSet argument.Arguments, parsers ...argument.Parser,
	) (argument.Arguments, <-chan error) {
		var (
			options = request.Options
			errs    = make(chan error)
		)
		if !hasUserDefinedOptions(options) {
			// If we have nothing to process,
			// just relay inputs as outputs.
			close(errs)
			return argsToSet, errs
		}
		unsetArgs := make(chan argument.Argument, cap(argsToSet))
		go func() {
			defer close(unsetArgs)
			defer close(errs)
			for {
				select {
				case argument, ok := <-argsToSet:
					if !ok {
						return
					}
					var (
						// NOTE: The cmdslibs stores values via the option's primary name.
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
							continue
						case <-ctx.Done():
							return
						}
					}
				case <-ctx.Done():
					return
				}
			}
		}()
		return unsetArgs, errs
	}
}

func assignOption(arg argument.Argument, cmdsArg any, parsers ...argument.Parser) error {
	if stringsArg, ok := cmdsArg.([]string); ok {
		parsedArg, err := argument.ParseStrings(arg, stringsArg, parsers...)
		if err != nil {
			return err
		}
		return argument.Assign(arg, parsedArg)
	}
	return argument.Assign(arg, cmdsArg)
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
