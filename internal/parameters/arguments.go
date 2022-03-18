package parameters

import (
	"context"
	"encoding/csv"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	. "github.com/djdv/go-filesystem-utils/internal/generic"
)

type (
	// ParseFunc receives a string representation of the data value,
	// and should return a typed Go value of it.
	ParseFunc func(argument string) (value interface{}, _ error)
	// TypeParser is the binding of a type with its corresponding parser function.
	TypeParser struct {
		reflect.Type
		ParseFunc
	}
	typeParsers []TypeParser

	// Argument is the pairing of a Parameter with a Go variable.
	// The value is typically a pointer to a field within a Settings struct,
	// but any abstract reference value is allowed.
	Argument struct {
		Parameter
		ValueReference interface{}
	}
	Arguments <-chan Argument
)

func (parsers typeParsers) Index(typ reflect.Type) *TypeParser {
	for _, parser := range parsers {
		if parser.Type == typ {
			return &parser
		}
	}
	return nil
}

// Parse will populate `set` using values returned by each `SetFunc`.
// Value sources are queried in the same order they're provided.
// If a setting cannot be set by one source,
// it's reference is relayed to the next `SetFunc`.
func Parse(ctx context.Context, set Settings,
	setFuncs []SetFunc, parsers ...TypeParser,
) error {
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	unsetArgs, generatorErrs, err := argsFromSettings(subCtx, set)
	if err != nil {
		return err
	}

	const generatorChanCount = 1
	errChans := make([]<-chan error, 0, len(setFuncs)+generatorChanCount)
	errChans = append(errChans, generatorErrs)

	for _, setter := range setFuncs {
		var errCh <-chan error
		unsetArgs, errCh = setter(subCtx, unsetArgs, parsers...)
		errChans = append(errChans, errCh)
	}

	var (
		errs  = CtxMerge(subCtx, errChans...)
		drain = func(Argument) error { return nil }
	)
	if err := ForEachOrError(subCtx, unsetArgs, errs, drain); err != nil {
		return fmt.Errorf("Parse encountered an error: %w", err)
	}
	return subCtx.Err()
}

func argsFromSettings(ctx context.Context, settings Settings) (Arguments, errorCh, error) {
	typ, err := checkType(settings)
	if err != nil {
		return nil, nil, err
	}

	var (
		parameters         = settings.Parameters()
		fields, fieldsErrs = generateSettingsFields(ctx, typ, parameters)

		arguments = make(chan Argument, cap(fields))
		errs      = make(chan error)
	)
	go func() {
		defer close(arguments)
		defer close(errs)
		var (
			paramIndex int
			firstErr   error
			setVal     = reflect.ValueOf(settings).Elem()
			fieldToArg = func(field reflect.StructField) (argument Argument, _ error) {
				if firstErr != nil {
					return argument, firstErr
				}
				var (
					ref      interface{}
					fieldVal = setVal.FieldByIndex(field.Index)
				)
				if ref, firstErr = referenceFromField(field, fieldVal); firstErr != nil {
					return argument, firstErr
				}
				argument = Argument{
					Parameter:      parameters[paramIndex],
					ValueReference: ref,
				}
				paramIndex++
				return argument, nil
			}
		)

		ProcessResults(ctx, fields, arguments, errs, fieldToArg)

		if ctx.Err() == nil { // Don't validate if we're canceled.
			expected := len(parameters)
			if err := checkParameterCount(paramIndex, expected, typ, parameters); err != nil {
				select {
				case errs <- err:
				case <-ctx.Done():
				}
			}
		}
	}()

	return arguments, CtxMerge(ctx, fieldsErrs, errs), nil
}

func generateSettingsFields(ctx context.Context,
	typ reflect.Type, parameters Parameters,
) (structFields, errorCh) {
	subCtx, cancel := context.WithCancel(ctx)
	var (
		baseFields = generateFields(subCtx, typ)
		allFields  = expandFields(subCtx, baseFields)

		tag                   = newStructTagPair(settingsTagKey, settingsTagValue)
		taggedFields, tagErrs = fieldsAfterTag(subCtx, tag, allFields)

		paramCount    = len(parameters)
		reducedFields = CtxTakeAndCancel(subCtx, cancel, paramCount, taggedFields)
	)

	return reducedFields, tagErrs
}

func assignToArgument(arg Argument, value interface{}, parsers ...TypeParser) error {
	var (
		leftValue  = reflect.ValueOf(arg.ValueReference).Elem()
		leftType   = leftValue.Type()
		rightValue = reflect.ValueOf(value)
		rightType  = rightValue.Type()
	)

	if canAssignDirectly(leftType, rightType) {
		leftValue.Set(rightValue)
		return nil
	}

	assigned, err := maybeAssignCustom(leftType, leftValue, value, parsers)
	if err != nil {
		return fmt.Errorf("custom handler: %w", err)
	}
	if assigned {
		return nil
	}

	convertedValue, err := maybeConvert(leftType, rightType, rightValue, parsers)
	if err != nil {
		return fmt.Errorf("type conversion: %w", err)
	}
	if convertedValue != nil {
		rightValue = *convertedValue
		rightType = rightValue.Type()
	}

	if !leftType.AssignableTo(rightType) {
		err := fmt.Errorf("%w: `%#v` to %v",
			errUnassignable, rightValue.Interface(), leftType,
		)
		return err
	}

	leftValue.Set(rightValue)
	return nil
}

func canAssignDirectly(leftType, rightType reflect.Type) bool {
	return leftType == rightType ||
		(leftType.Kind() == reflect.Interface &&
			rightType.Implements(leftType))
}

func maybeConvert(leftType, rightType reflect.Type, rightValue reflect.Value,
	parsers typeParsers,
) (*reflect.Value, error) {
	if leftKind := leftType.Kind(); leftKind == reflect.Slice ||
		leftKind == reflect.Array {
		// Reduce the set to only the one we need.
		if parser := parsers.Index(leftType.Elem()); parser != nil {
			parsers = []TypeParser{*parser}
		}
		return parseVector(leftType, rightValue, parsers)
	}

	if rightType.Kind() == reflect.String {
		goString := rightValue.Interface().(string)
		reflectValue, err := parseString(leftType, goString)
		if err != nil {
			return nil, err
		}
		if reflectValue != nil {
			rightValue = *reflectValue
			rightType = rightValue.Type()
		}
	}
	return maybeConvertBuiltin(leftType, rightType, rightValue)
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

func parseString(typ reflect.Type, value string) (*reflect.Value, error) {
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
			errUnexpectedType, kind,
		)
	}
	if err != nil {
		return nil, err
	}
	reflectValue := reflect.ValueOf(typedValue)
	return &reflectValue, nil
}

func parseVector(typ reflect.Type, value reflect.Value, parsers typeParsers) (*reflect.Value, error) {
	kind := typ.Kind()
	if kind != reflect.Slice &&
		kind != reflect.Array {
		err := fmt.Errorf(
			"%w:"+
				" got: `%v`"+
				" want: `%v` or `%v`",
			errUnexpectedType,
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
			errUnexpectedType,
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
		if err := assignToArgument(indexAsArg, argStr, parsers...); err != nil {
			return nil, err
		}
	}
	if kind == reflect.Array {
		return &arrayValue, nil
	}
	sliceValue := arrayValue.Slice(0, vectorLen)
	return &sliceValue, nil
}

func maybeAssignCustom(leftType reflect.Type, leftValue reflect.Value,
	rightValue interface{}, parsers typeParsers) (bool, error,
) {
	parser := parsers.Index(leftType)
	if parser == nil {
		return false, nil
	}
	if err := assignCustomType(parser.ParseFunc, leftValue, rightValue); err != nil {
		return false, fmt.Errorf("failed to assign to `%v`: %w", leftType, err)
	}
	return true, nil
}

func assignCustomType(parse ParseFunc, leftValue reflect.Value, rightValue interface{}) error {
	valueStr, isString := rightValue.(string)
	if !isString {
		return fmt.Errorf("%w for parser argument - got: %T want: %T",
			errUnexpectedType, valueStr, rightValue,
		)
	}
	customValue, err := parse(valueStr)
	if err != nil {
		return fmt.Errorf("failed to parse: %w", err)
	}
	leftValue.Set(reflect.ValueOf(customValue))
	return nil
}
