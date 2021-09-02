package parameters

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/multiformats/go-multiaddr"
)

// SettingsFromEnvironment uses the process environment as a source for settings values.
func SettingsFromEnvironment() SettingsSource {
	return (*procenvSettingsSource)(nil)
}

type procenvSettingsSource struct{}

func (*procenvSettingsSource) setEach(ctx context.Context,
	argsToSet ArgumentList,
	inputErrors <-chan error) (ArgumentList, <-chan error) {
	return setEach(ctx, fromEnv, argsToSet, inputErrors)
}

func fromEnv(argument *Argument) (provided bool, err error) {
	var (
		envStringValue string
		envKey         = argument.Parameter.Environment()
	)
	if envStringValue, provided = os.LookupEnv(envKey); !provided {
		return
	}

	// The type of the value referenced in the argument,
	// determines how we choose to parse
	// the environment's string value.
	var (
		argumentTyped interface{}
		assignErr     = fmt.Errorf("parameter `%s`", envKey)
	)
	switch argVal := argument.ValueReference.(type) {
	case *bool:
		argumentTyped, err = strconv.ParseBool(envStringValue)
	case *int, *int64:
		argumentTyped, err = strconv.ParseInt(envStringValue, 0, 64)
	case *uint, *uint64:
		argumentTyped, err = strconv.ParseUint(envStringValue, 0, 64)
	case *float32,
		*float64:
		argumentTyped, err = strconv.ParseFloat(envStringValue, 64)
	case *complex64,
		*complex128:
		argumentTyped, err = strconv.ParseComplex(envStringValue, 128)
	case *string, *multiaddr.Multiaddr:
		argumentTyped = envStringValue
	case *[]string:
		argumentTyped, err = csv.NewReader(strings.NewReader(envStringValue)).Read()
	default:
		err = fmt.Errorf("invalid argument type %T", argVal)
	}
	if err != nil {
		err = fmt.Errorf("%w: %s", assignErr, err)
		return
	}
	if err = assignToArgument(argument, argumentTyped); err != nil {
		err = fmt.Errorf("%w: %s", assignErr, err)
	}
	return
}
