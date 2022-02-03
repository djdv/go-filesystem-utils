package parameters

import (
	"context"
	"fmt"
	"os"
	"reflect"
)

// SettingsFromEnvironment uses the process environment as a source for settings values.
func SettingsFromEnvironment() SetFunc {
	return func(ctx context.Context, argsToSet Arguments,
		parsers ...TypeParser,
	) (Arguments, <-chan error) {
		return setEach(ctx, fromEnv(parsers...), argsToSet)
	}
}

func fromEnv(parsers ...TypeParser) providedFunc {
	return func(arg Argument) (provided bool, _ error) {
		var (
			envKey         string
			envStringValue string
			envKeys        = append([]string{
				arg.Parameter.Name(Environment),
			},
				arg.Parameter.Aliases(Environment)...,
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
		if err := assignToArgument(arg, typedEnvVar, parsers...); err != nil {
			return false, fmt.Errorf(
				"failed to assign from environment variable `%s` (%v): %w",
				envKey, typedEnvVar, err,
			)
		}
		return provided, nil
	}
}

func assertEnvValue(goValueRef interface{}, envValue string) (interface{}, error) {
	leftType := reflect.TypeOf(goValueRef).Elem()
	reflectValue, err := parseString(leftType, envValue)
	if err != nil {
		err = fmt.Errorf("could not assert value: %w (for reference %T)",
			err, goValueRef,
		)
		return nil, err
	}
	return reflectValue.Interface(), nil
}
