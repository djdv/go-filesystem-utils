package argument

import (
	"context"
	"fmt"
	"reflect"

	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/runtime"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameter"
)

type (
	// TODO: English
	// Argument is the binding of a Parameter with a Go value.
	// (The value is typically a pointer to a `Settings` struct member,
	// but may be any settable value.)
	Argument  generic.Couple[parameter.Parameter, any]
	Arguments <-chan Argument

	// TODO: rename - SetValue
	// SetFunc should attempt to assign to each `Argument.ValueReference` it receives.
	// (Typically by utilizing the `Argument.Parameter.Name` as a key to a value store.)
	// SetFunc must send all unassigned `Argument`s (if any) to its output channel.
	SetFunc func(context.Context, Arguments, ...TypeParser) (unsetArgs Arguments, _ <-chan error)

	// TODO: rename ParseArgument
	// ParseFunc receives a string representation of the data value,
	// and returns a typed Go value of it.
	ParseFunc func(argument string) (value any, _ error)

	// TypeParser is the binding of a type with its corresponding parse function.
	TypeParser struct {
		reflect.Type
		ParseFunc
	}

	errors = <-chan error
)

func argsFromSettings[
	setPtr runtime.SettingsType[settings],
	settings any,
](ctx context.Context,
	set setPtr,
) (Arguments, errors, error) {
	baseFields, err := runtime.ReflectFields[setPtr](ctx)
	if err != nil {
		return nil, nil, err
	}
	var (
		allFields   = expandEmbedded(ctx, baseFields)
		params      = setPtr.Parameters(nil, ctx)
		fieldParams = generic.CtxBoth(ctx, allFields, params)

		arguments = make(chan Argument, cap(params))
		errs      = make(chan error)
	)
	go func() {
		defer close(arguments)
		defer close(errs)
		structValue := reflect.ValueOf(set).Elem()
		for fieldParam := range fieldParams {
			var (
				field               = fieldParam.Left
				param               = fieldParam.Right
				fieldValue          = structValue.FieldByIndex(field.Index)
				valueReference, err = referenceFromField(field, fieldValue)
			)
			if err != nil {
				select {
				case errs <- err:
				case <-ctx.Done():
				}
				return
			}
			argument := Argument{
				Left:  param,
				Right: valueReference,
			}
			select {
			case arguments <- argument:
			case <-ctx.Done():
				return
			}
		}
	}()

	return arguments, errs, nil
}

func maybeGetParser(typ reflect.Type, parsers ...TypeParser) *TypeParser {
	for _, parser := range parsers {
		if parser.Type == typ {
			return &parser
		}
	}
	return nil
}

// TODO: outdated? check comment
// / Parse will populate `set` using values returned by each `SetFunc`.
// Value sources are queried in the same order they're provided.
// If a setting cannot be set by one source,
// it's reference is relayed to the next `SetFunc`.
func Parse[setIntf runtime.SettingsType[set], set any](ctx context.Context,
	setFuncs []SetFunc, parsers ...TypeParser,
) (*set, error) {
	settingsPointer := new(set)
	unsetArgs, settingsErrs, err := argsFromSettings[setIntf](ctx, settingsPointer)
	if err != nil {
		return nil, err
	}

	errChans := append(
		make([]errors, 0, len(setFuncs)+1),
		settingsErrs,
	)
	for _, setter := range setFuncs {
		var errChan errors
		unsetArgs, errChan = setter(ctx, unsetArgs, parsers...)
		errChans = append(errChans, errChan)
	}

	const parseErrFmt = "Parse encountered an error: %w"
	for err := range generic.CtxMerge(ctx, errChans...) {
		return nil, fmt.Errorf(parseErrFmt, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf(parseErrFmt, err)
	}
	return settingsPointer, nil
}

func Assign(arg Argument, value any) error {
	targetValue := reflect.ValueOf(arg.Right).Elem()
	if !targetValue.CanSet() {
		return fmt.Errorf("%w: `reflect.Value.CanSet` returned false for argument reference",
			runtime.ErrUnassignable,
		)
	}

	var (
		targetType   = targetValue.Type()
		reflectValue = reflect.ValueOf(value)
		reflectType  = reflectValue.Type()
	)
	if !targetType.AssignableTo(reflectType) {
		err := fmt.Errorf("%w: `%#v` to %v",
			runtime.ErrUnassignable, value, targetType,
		)
		return err
	}
	targetValue.Set(reflectValue)
	return nil
}
