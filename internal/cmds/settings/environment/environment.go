package environment

import (
	"context"
	"fmt"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

// ValueSource uses the process environment as a source for settings values.
func ValueSource() runtime.SetFunc {
	return func(ctx context.Context, argsToSet runtime.Arguments,
		parsers ...runtime.TypeParser,
	) (runtime.Arguments, <-chan error) {
		var (
			unsetArgs = make(chan runtime.Argument, cap(argsToSet))
			errs      = make(chan error)
		)
		go func() {
			defer close(unsetArgs)
			defer close(errs)
			assignOrRelay := func(arg runtime.Argument) error {
				var (
					envKeys              = getKeys(arg)
					provided, key, value = maybeGetValue(envKeys...)
				)
				if !provided {
					select {
					case unsetArgs <- arg:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				parsedArg, err := runtime.ParseStrings(arg, value, parsers...)
				if err != nil {
					return fmt.Errorf(
						"failed to assign from environment variable `%s:%s`: %w",
						key, value, err,
					)
				}
				return runtime.Assign(arg, parsedArg)
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

func getKeys(arg runtime.Argument) []string {
	return append([]string{
		arg.Name(parameters.Environment),
	},
		arg.Aliases(parameters.Environment)...,
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
