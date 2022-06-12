package argument

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/runtime"
)

// ParseStrings interprets a string as/into a Go value
// or a list of strings into a list of values.
func ParseStrings[stringish string | []string](arg Argument, value stringish,
	parsers ...Parser,
) (any, error) {
	typ := reflect.TypeOf(arg.ValueReference)
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	if plural, ok := any(value).([]string); ok {
		return parseStrings(typ, plural, parsers...)
	}

	singular := any(value).(string)
	if parser := maybeGetParser(typ, parsers...); parser != nil {
		return parser(singular)
	}
	return parseBuiltin(typ, singular)
}

func parseStrings(typ reflect.Type, values []string, parsers ...Parser) (any, error) {
	vectorValue, err := makeVector(typ, len(values))
	if err != nil {
		return nil, err
	}
	var (
		elemType       = typ.Elem()
		userParser     = maybeGetParser(elemType, parsers...)
		haveUserParser = userParser != nil
		parse          func(s string) (any, error)
	)
	if haveUserParser {
		parse = userParser
	} else {
		parse = func(s string) (any, error) {
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

func referenceFromField(field reflect.StructField, fieldValue reflect.Value) (any, error) {
	if !fieldValue.CanSet() {
		err := fmt.Errorf("%w"+
			" field %s in type `%s` is not settable",
			runtime.ErrUnassignable,
			field.Name, field.Type.Name(),
		)
		if !field.IsExported() {
			err = fmt.Errorf("%w (the field is not exported)",
				err)
		}
		return nil, err
	}
	return fieldValue.Addr().Interface(), nil
}

// parseBuiltin interprets the string as/into the provided Go type.
func parseBuiltin(typ reflect.Type, value string) (any, error) {
	switch kind := typ.Kind(); kind {
	case reflect.Bool:
		return strconv.ParseBool(value)
	case reflect.Int:
		return strconv.Atoi(value)
	case reflect.Int8:
		parsedInt, err := strconv.ParseInt(value, 0, 8)
		return int8(parsedInt), err
	case reflect.Int16:
		parsedInt, err := strconv.ParseInt(value, 0, 16)
		return int16(parsedInt), err
	case reflect.Int32:
		parsedInt, err := strconv.ParseInt(value, 0, 32)
		return int32(parsedInt), err
	case reflect.Int64:
		return strconv.ParseInt(value, 0, 64)
	case reflect.Uint:
		parsedUint, err := strconv.ParseUint(value, 0, 0)
		return uint(parsedUint), err
	case reflect.Uint8:
		parsedUint, err := strconv.ParseUint(value, 0, 8)
		return int8(parsedUint), err
	case reflect.Uint16:
		parsedUint, err := strconv.ParseUint(value, 0, 16)
		return int16(parsedUint), err
	case reflect.Uint32:
		parsedUint, err := strconv.ParseUint(value, 0, 32)
		return int32(parsedUint), err
	case reflect.Uint64:
		return strconv.ParseUint(value, 0, 64)
	case reflect.Float32:
		parsedFloat, err := strconv.ParseFloat(value, 32)
		return float32(parsedFloat), err
	case reflect.Float64:
		return strconv.ParseFloat(value, 64)
	case reflect.Complex64:
		parsedComplex, err := strconv.ParseComplex(value, 64)
		return complex64(parsedComplex), err
	case reflect.Complex128:
		return strconv.ParseComplex(value, 128)
	default:
		return nil, fmt.Errorf("%w: no parser for value type: %s kind: %s",
			runtime.ErrUnexpectedType, typ, kind,
		)
	}
}

// makeVector takes in an array or slice type and returns a new value for it.
func makeVector(typ reflect.Type, elemCount int) (*reflect.Value, error) {
	switch vectorKind := typ.Kind(); vectorKind {
	case reflect.Array:
		vectorLen := typ.Len()
		if elemCount > vectorLen {
			err := fmt.Errorf("array of size %d cannot fit %d elements",
				vectorLen, elemCount,
			)
			return nil, err
		}
		arrayValue := reflect.New(typ).Elem()
		return &arrayValue, nil
	case reflect.Slice:
		sliceValue := reflect.MakeSlice(typ, elemCount, elemCount)
		return &sliceValue, nil
	default:
		err := fmt.Errorf(
			"%w:"+
				" got: `%s`"+
				" want: `%s` or `%s`",
			runtime.ErrUnexpectedType,
			vectorKind,
			reflect.Slice, reflect.Array,
		)
		return nil, err
	}
}
