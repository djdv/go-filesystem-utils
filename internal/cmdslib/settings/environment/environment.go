package environment

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings/runtime"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

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

	typedEnvVar, err := assertEnvValue(arg.ValueReference, envStringValue)
	if err != nil {
		return false, fmt.Errorf(
			"failed to parse environment variable `%s`: %w",
			envKey, err,
		)
	}
	if err := runtime.AssignToArgument(arg, typedEnvVar, parsers...); err != nil {
		return false, fmt.Errorf(
			"failed to assign from environment variable `%s` (%v): %w",
			envKey, typedEnvVar, err,
		)
	}
	return provided, nil
}

func assertEnvValue(goValueRef interface{}, envValue string) (interface{}, error) {
	leftType := reflect.TypeOf(goValueRef).Elem()
	reflectValue, err := runtime.ParseString(leftType, envValue)
	if err != nil {
		err = fmt.Errorf("could not assert value (for reference %T): %w ",
			goValueRef, err,
		)
		return nil, err
	}
	return reflectValue.Interface(), nil
}
