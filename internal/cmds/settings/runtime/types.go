package runtime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

// TODO: Name. SettingsType?
type SettingsConstraint[Settings any] interface {
	*Settings           // Type parameter must be pointer to struct
	parameters.Settings // which implements the Settings interface.
}

var (
	// TODO: [review] should these be exported? Probably, but double check.
	ErrUnassignable   = errors.New("cannot assign")
	ErrUnexpectedType = errors.New("unexpected type")
)

func argsFromSettings[setPtr SettingsConstraint[settings], settings any](ctx context.Context,
	set setPtr,
) (Arguments, <-chan error, error) {
	fields, fieldsErrs, err := bindParameterFields[setPtr](ctx)
	if err != nil {
		return nil, nil, err
	}

	var (
		arguments = make(chan Argument, cap(fields))
		errs      = make(chan error)
	)
	go func() {
		defer close(arguments)
		defer close(errs)
		var (
			firstErr   error
			setVal     = reflect.ValueOf(set).Elem()
			fieldToArg = func(param ParamField) (argument Argument, _ error) {
				if firstErr != nil {
					return argument, firstErr
				}
				var (
					ref       interface{}
					fieldVal  = setVal.FieldByIndex(param.Index)
					field     = param.StructField
					parameter = param.Parameter
				)
				if ref, firstErr = referenceFromField(field, fieldVal); firstErr != nil {
					return argument, firstErr
				}
				argument = Argument{
					Parameter:      parameter,
					ValueReference: ref,
				}
				return argument, nil
			}
		)
		generic.ProcessResults(ctx, fields, arguments, errs, fieldToArg)
	}()

	return arguments, generic.CtxMerge(ctx, fieldsErrs, errs), nil
}

func checkType[settings any]() (reflect.Type, error) {
	typ := reflect.TypeOf((*settings)(nil)).Elem()
	if kind := typ.Kind(); kind != reflect.Struct {
		err := fmt.Errorf("%w:"+
			" got: `%s`"+
			" want: `struct`",
			ErrUnexpectedType,
			kind,
		)
		return nil, err
	}
	return typ, nil
}

func referenceFromField(field reflect.StructField, fieldValue reflect.Value) (interface{}, error) {
	if !fieldValue.CanSet() {
		err := fmt.Errorf("%w"+
			" field %s in type `%s` is not settable",
			ErrUnassignable,
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

// parseBuiltin parses the string value as/to the provided Go type.
func parseBuiltin(typ reflect.Type, value string) (interface{}, error) {
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
			ErrUnexpectedType, typ, kind,
		)
	}
}

func parseVector(typ reflect.Type, values []string, parsers ...TypeParser) (interface{}, error) {
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
			ErrUnexpectedType,
			vectorKind,
			reflect.Slice, reflect.Array,
		)
		return nil, err
	}
}
