package environment

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/argument"
	"github.com/djdv/go-filesystem-utils/internal/parameter"
)

// TODO: update docs - 'IS a setFunc'
// ValueSource uses the process environment as a source for settings values.
func ValueSource(ctx context.Context, argsToSet argument.Arguments,
	parsers ...argument.Parser,
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
			sendErr := func(err error) bool {
				select {
				case errs <- fmt.Errorf(
					errFmt+"`%s:%s`: %w",
					key, value, err,
				):
					return true
				case <-ctx.Done():
					return false
				}
			}
			valueType := reflect.TypeOf(arg.ValueReference)
			if valueType.Kind() == reflect.Pointer {
				valueType = valueType.Elem()
			}
			var (
				parsedArg any
				err       error
			)
			if valueType.Kind() == reflect.Slice {
				stringsValue, csvErr := csv.NewReader(strings.NewReader(value)).Read()
				if csvErr != nil {
					if !sendErr(csvErr) {
						return
					}
					continue
				}
				parsedArg, err = argument.ParseStrings(arg, stringsValue, parsers...)
			} else {
				parsedArg, err = argument.ParseStrings(arg, value, parsers...)
			}
			if err != nil {
				if !sendErr(err) {
					return
				}
				continue
			}
			if err := argument.Assign(arg, parsedArg); err != nil {
				if !sendErr(err) {
					return
				}
				continue
			}
		}
	}()
	return unsetArgs, errs
}

func getKeys(arg argument.Argument) []string {
	param := arg.Parameter
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
