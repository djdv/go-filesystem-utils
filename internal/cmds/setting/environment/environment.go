package environment

import (
	"context"
	"fmt"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/argument"
	"github.com/djdv/go-filesystem-utils/internal/parameter"
)

// ValueSource uses the process environment as a source for settings values.
func ValueSource() argument.SetFunc {
	return func(ctx context.Context, argsToSet argument.Arguments,
		parsers ...argument.TypeParser,
	) (argument.Arguments, <-chan error) {
		var (
			unsetArgs = make(chan argument.Argument, cap(argsToSet))
			errs      = make(chan error)
		)
		go func() {
			defer close(unsetArgs)
			defer close(errs)
			const errFmt = "failed to assign from environment variable"
			for arg := range argsToSet {
				var (
					envKeys              = getKeys(arg)
					provided, key, value = maybeGetValue(envKeys...)
				)
				if !provided {
					select {
					case unsetArgs <- arg:
						continue
					case <-ctx.Done():
						return
					}
				}
				// TODO: cleanup err logic - wrap parseAndAssign(...)?
				parsedArg, err := argument.ParseStrings(arg, value, parsers...)
				if err != nil {
					select {
					case errs <- fmt.Errorf(
						errFmt+"`%s:%s`: %w",
						key, value, err,
					):
					case <-ctx.Done():
					}
				}
				if err := argument.Assign(arg, parsedArg); err != nil {
					select {
					case errs <- fmt.Errorf(
						errFmt+"`%s:%s`: %w",
						key, value, err,
					):
					case <-ctx.Done():
					}
				}
			}
		}()
		return unsetArgs, errs
	}
}

func getKeys(arg argument.Argument) []string {
	param := arg.Left
	return append([]string{
		param.Name(parameter.Environment),
	},
		param.Aliases(parameter.Environment)...,
	)
}

func maybeGetValue(keys ...string) (provided bool, key, value string) {
	for _, key = range keys {
		if value, provided = os.LookupEnv(key); provided {
			return
		}
	}
	return
}
