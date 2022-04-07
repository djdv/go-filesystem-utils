package runtime

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	. "github.com/djdv/go-filesystem-utils/internal/generic"
)

type errCh = <-chan error

var (
	ErrUnassignable   = errors.New("cannot assign")
	ErrUnexpectedType = errors.New("unexpected type")
	errMultiPointer   = errors.New("multiple layers of indirection (not supported)")
)

func ArgsFromSettings[settings any, setPtr SettingsConstraint[settings]](ctx context.Context, set setPtr) (Arguments, errCh, error) {
	fields, fieldsErrs, err := BindParameterFields[settings, setPtr](ctx)
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
		ProcessResults(ctx, fields, arguments, errs, fieldToArg)
	}()

	return arguments, CtxMerge(ctx, fieldsErrs, errs), nil
}

func checkType[settings any, structPtr SettingsConstraint[settings]]() (reflect.Type, error) {
	var (
		pointerType = reflect.TypeOf((*structPtr)(nil)).Elem()
		makeErr     = func() error {
			var instance structPtr
			return fmt.Errorf("%w:"+
				" got: %T"+
				" want: pointer to struct",
				ErrUnexpectedType,
				instance,
			)
		}
	)
	if pointerType.Kind() != reflect.Ptr {
		return nil, makeErr()
	}
	if structType := pointerType.Elem(); structType.Kind() == reflect.Struct {
		return structType, nil
	}
	return nil, makeErr()
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

func maybeConvert(leftType, rightType reflect.Type, rightValue reflect.Value,
	parsers typeParsers,
) (*reflect.Value, error) {
	specialValue, err := handlePtrArgTypes(leftType, rightType, rightValue, parsers)
	if err != nil {
		return nil, err
	}
	if specialValue != nil {
		return specialValue, nil
	}
	return maybeConvertBuiltin(leftType, rightType, rightValue)
}

func handlePtrArgTypes(leftType, rightType reflect.Type, rightValue reflect.Value,
	parsers typeParsers,
) (*reflect.Value, error) {
	leftKind := leftType.Kind()
	if leftKind == reflect.Slice ||
		leftKind == reflect.Array {
		return handleVectorArgs(leftType, rightValue, parsers)
	}

	goString, argIsString := rightValue.Interface().(string)
	if argIsString {
		convertedValue, err := ParseString(leftType, goString)
		if err != nil {
			return nil, err
		}
		if convertedValue != nil {
			var (
				rVal          = *convertedValue
				convertedType = rVal.Type()
			)
			return maybeConvertBuiltin(leftType, convertedType, rVal)
		}
	}

	return nil, nil
}

func handleVectorArgs(leftType reflect.Type, rightValue reflect.Value,
	parsers typeParsers,
) (*reflect.Value, error) {
	// Reduce the set to only the one we need.
	if parser := parsers.Index(leftType.Elem()); parser != nil {
		parsers = []TypeParser{*parser}
	}
	return parseVector(leftType, rightValue, parsers)
}

func parseVector(typ reflect.Type, value reflect.Value, parsers typeParsers) (*reflect.Value, error) {
	kind := typ.Kind()
	if kind != reflect.Slice &&
		kind != reflect.Array {
		err := fmt.Errorf(
			"%w:"+
				" got: `%v`"+
				" want: `%v` or `%v`",
			ErrUnexpectedType,
			kind,
			reflect.Slice, reflect.Array,
		)
		return nil, err
	}

	var (
		goValue          = value.Interface()
		valueStrings, ok = goValue.([]string)
	)
	if !ok {
		err := fmt.Errorf(
			"%w:"+
				" got: %T for type %v"+
				" want: %T",
			ErrUnexpectedType,
			goValue, typ,
			valueStrings,
		)
		return nil, err
	}

	var (
		vectorLen  = value.Len()
		arrayType  = reflect.ArrayOf(vectorLen, typ.Elem())
		arrayValue = reflect.New(arrayType).Elem()
	)
	for i, argStr := range valueStrings {
		var (
			indexPtr   = arrayValue.Index(i).Addr().Interface()
			indexAsArg = Argument{ValueReference: indexPtr}
		)
		if err := AssignToArgument(indexAsArg, argStr, parsers...); err != nil {
			return nil, err
		}
	}
	if kind == reflect.Array {
		return &arrayValue, nil
	}
	sliceValue := arrayValue.Slice(0, vectorLen)
	return &sliceValue, nil
}

func maybeConvertBuiltin(leftType, rightType reflect.Type, rightValue reflect.Value) (*reflect.Value, error) {
	switch kind := leftType.Kind(); kind {
	case reflect.Bool:
		return &rightValue, nil
	case reflect.String,
		reflect.Struct,
		reflect.Interface,
		reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64,
		reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Float32,
		reflect.Float64,
		reflect.Complex64,
		reflect.Complex128:
		if convertableTo := rightType.ConvertibleTo(leftType); convertableTo {
			converted := rightValue.Convert(leftType)
			return &converted, nil
		}
	case reflect.Ptr:
		err := fmt.Errorf(
			"%w: left type (%v)",
			errMultiPointer, leftType,
		)
		return nil, err
	}
	return nil, nil
}

func ParseString(typ reflect.Type, value string) (*reflect.Value, error) {
	const (
		numberSize  = 64
		complexSize = 128
	)
	var (
		typedValue interface{}
		err        error
	)
	switch kind := typ.Kind(); kind {
	case reflect.String,
		reflect.Interface:
		typedValue = value
	case reflect.Bool:
		typedValue, err = strconv.ParseBool(value)
	case reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64:
		typedValue, err = strconv.ParseInt(value, 0, numberSize)
	case reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64:
		typedValue, err = strconv.ParseUint(value, 0, numberSize)
	case reflect.Float32,
		reflect.Float64:
		typedValue, err = strconv.ParseFloat(value, numberSize)
	case reflect.Complex64,
		reflect.Complex128:
		typedValue, err = strconv.ParseComplex(value, complexSize)
	case reflect.Slice,
		reflect.Array:
		typedValue, err = csv.NewReader(strings.NewReader(value)).Read()
	default:
		err = fmt.Errorf("%w: no parser for value kind %v",
			ErrUnexpectedType, kind,
		)
	}
	if err != nil {
		return nil, err
	}
	reflectValue := reflect.ValueOf(typedValue)
	return &reflectValue, nil
}
