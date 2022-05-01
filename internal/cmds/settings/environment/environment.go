package environment

import (
	"context"
	"fmt"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

// TODO: name convention for these; `SetFunc`, `FromEnv`?
// SettingsFromEnvironment uses the process environment as a source for settings values.
func SettingsFromEnvironment() runtime.SetFunc {
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
			fn := func(unsetArg runtime.Argument) (runtime.Argument, error) {
				provided, err := fromEnv(unsetArg, parsers...)
				if err != nil {
					return unsetArg, err
				}
				if provided { // We're going to process this, skip relaying it.
					return unsetArg, generic.ErrSkip
				}
				return unsetArg, nil
			}

			generic.ProcessResults(ctx, argsToSet, unsetArgs, errs, fn)
		}()
		return unsetArgs, errs
	}
}

func fromEnv(arg runtime.Argument, parsers ...runtime.TypeParser) (provided bool, _ error) {
	var (
		envKey         string
		envStringValue string
		envKeys        = append([]string{
			arg.Parameter.Name(parameters.Environment),
		},
			arg.Parameter.Aliases(parameters.Environment)...,
		)
	)
	for _, key := range envKeys {
		envStringValue, provided = os.LookupEnv(key)
		if provided {
			envKey = key
			break
		}
	}
	if !provided {
		return false, nil
	}
	if err := runtime.ParseAndAssign(arg, envStringValue, parsers...); err != nil {
		return false, fmt.Errorf(
			"failed to assign from environment variable `%s:%s`: %w",
			envKey, envStringValue, err,
		)
	}
	return provided, nil
}
