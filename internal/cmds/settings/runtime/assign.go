package runtime

import (
	"encoding/csv"
	"fmt"
	"reflect"
	"strings"
)

// TODO: rename source file?

// TODO: [refactor] Some callers of this don't need this wrapper
// and can now call Parse or Assign directly.
//
// TODO: [Ame] English
// TODO: Document `[]string` input.
// ParseAndAssign takes in either a direct Go value or a string representation of it
// and assigns it to the Argument's reference.
func ParseAndAssign(arg Argument, value interface{}, parsers ...TypeParser) error {
	targetValue := reflect.ValueOf(arg.ValueReference).Elem()
	if !targetValue.CanSet() {
		return fmt.Errorf("%w: `reflect.Value.CanSet` returned false for argument reference",
			ErrUnassignable,
		)
	}

	targetType := targetValue.Type()
	parsedValue, err := parseValue(targetType, value, parsers...)
	if err != nil {
		return err
	}
	targetValue.Set(parsedValue)
	return nil
}

func parseValue(targetType reflect.Type, value interface{},
	parsers ...TypeParser,
) (reflect.Value, error) {
	var (
		userParser     = maybeGetParser(targetType, parsers...)
		haveUserParser = userParser != nil
		reflectValue   = reflect.ValueOf(value)
		valueType      = reflectValue.Type()
	)
	if !haveUserParser &&
		targetType.AssignableTo(valueType) {
		return reflectValue, nil
	}

	var (
		parsedGoValue interface{}
		err           error
	)
	switch stringish := value.(type) {
	case string:
		if haveUserParser {
			parsedGoValue, err = userParser.ParseFunc(stringish)
		} else {
			parsedGoValue, err = parseString(targetType, stringish, parsers...)
		}
	case []string:
		parsedGoValue, err = parseVector(targetType, stringish, parsers...)
	default:
		parsedGoValue, err = parseStringArray(targetType, valueType, reflectValue, parsers...)
	}
	if err != nil {
		return reflect.Value{}, err
	}

	var (
		parsedValue = reflect.ValueOf(parsedGoValue)
		parsedType  = parsedValue.Type()
	)
	if !targetType.AssignableTo(parsedType) {
		err := fmt.Errorf("%w: `%#v` to %v",
			ErrUnassignable, parsedGoValue, targetType,
		)
		return reflect.Value{}, err
	}
	return parsedValue, nil
}

func parseString(targetType reflect.Type, value string,
	parsers ...TypeParser,
) (interface{}, error) {
	if kind := targetType.Kind(); kind == reflect.Slice ||
		kind == reflect.Array {
		stringValues, err := csv.NewReader(strings.NewReader(value)).Read()
		if err != nil {
			return nil, err
		}
		return parseVector(targetType, stringValues, parsers...)
	}
	return parseBuiltin(targetType, value)
}

func parseStringArray(targetType, arrayType reflect.Type, value reflect.Value,
	parsers ...TypeParser,
) (interface{}, error) {
	if arrayType.Kind() != reflect.Array ||
		(arrayType.Elem().Kind() != reflect.String) {
		err := fmt.Errorf("%w: builtin parser accepts"+
			" `%s`, `%T`, or input argument's type (`%s`) but got %s",
			ErrUnassignable, reflect.String, ([]string)(nil), targetType, arrayType,
		)
		// TODO: we should check for ErrUnassignable in caller
		// and add the context of what's accepted there, not here.
		// This function doesn't accept []T, only the caller does.
		return nil, err
	}

	valueSlice := value.Slice(0, arrayType.Len()).Interface().([]string)
	return parseVector(targetType, valueSlice, parsers...)
}
