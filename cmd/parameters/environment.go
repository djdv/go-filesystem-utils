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

// TODO: dedupe with cmds, we only really need to define a (provided, err) func like env is doing
func (*procenvSettingsSource) setEach(ctx context.Context,
	argsToSet ArgumentList,
	inputErrors <-chan error) (ArgumentList, <-chan error) {
	var (
		unsetArgs = make(chan *Argument, cap(argsToSet))
		errors    = make(chan error, cap(inputErrors))
	)
	go func() {
		defer close(unsetArgs)
		defer close(errors)
		for argsToSet != nil ||
			inputErrors != nil {
			select {
			case argument, ok := <-argsToSet:
				if !ok {
					argsToSet = nil
					continue
				}
				provided, err := fromEnv(argument)
				if err != nil {
					select {
					case errors <- err:
					case <-ctx.Done():
					}
				}
				if provided {
					continue
				}
				select { // Relay parameter to next source.
				case unsetArgs <- argument:
				case <-ctx.Done():
				}
			case err, ok := <-inputErrors:
				if !ok {
					inputErrors = nil
					continue
				}
				// If we encounter an input error,
				// relay it and keep processing.
				// The caller may decide to cancel or not afterwards.
				select {
				case errors <- err:
				case <-ctx.Done():
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return unsetArgs, errors
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
