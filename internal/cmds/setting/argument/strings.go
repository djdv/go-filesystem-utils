package argument

import (
	"encoding/csv"
	goerrors "errors" // TODO: remove
	"reflect"
	"strings"
)

// TODO: better name?
// TODO: support Stringer?
type StringsConstraint interface{ string | []string }

func ParseStrings[stringish StringsConstraint](arg Argument, value stringish,
	parsers ...TypeParser,
) (any, error) {
	targetType := reflect.TypeOf(arg.Right).Elem()
	switch stringish := any(value).(type) {
	case string:
		userParser := maybeGetParser(targetType, parsers...)
		if userParser != nil {
			return userParser.ParseFunc(stringish)
		}
		return parseString(targetType, stringish, parsers...)
	case []string:
		return parseStrings(targetType, stringish, parsers...)
	default:
		return nil, goerrors.New("unexpected type") // TODO: real error
	}
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
		return parseStrings(targetType, stringValues, parsers...)
	}
	return parseBuiltin(targetType, value)
}

func parseStrings(typ reflect.Type, values []string, parsers ...TypeParser) (interface{}, error) {
	vectorValue, err := makeVector(typ, len(values))
	if err != nil {
		return nil, err
	}
	var (
		elemType       = typ.Elem()
		userParser     = maybeGetParser(elemType, parsers...)
		haveUserParser = userParser != nil
		parse          func(s string) (interface{}, error)
	)
	if haveUserParser {
		parse = userParser.ParseFunc
	} else {
		parse = func(s string) (interface{}, error) {
			return parseBuiltin(elemType, s)
		}
	}
	for i, stringValue := range values {
		goValue, err := parse(stringValue)
		if err != nil {
			return nil, err
		}
		vectorValue.Index(i).Set(reflect.ValueOf(goValue))
	}
	return vectorValue.Interface(), nil
}
