// Package options provides generic constructors for the cmdslib `Option` interface.
package options

import (
	"context"
	"fmt"
	"reflect"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

type (
	// NewOptionFunc should follow the same conventions
	// as `Option` constructors from the cmdslib pkg.
	//
	// I.e. the first argument is the primary name (e.g. `some name` => `--some-name`),
	// additional arguments are aliases (`n` => `-n`),
	// and the final argument is the description for the option (used in user facing help text).
	NewOptionFunc func(...string) cmds.Option

	// TypeConstructor is the binding of a type
	// with its corresponding `Option` constructor.
	TypeConstructor struct {
		reflect.Type
		NewOptionFunc
	}
)

// MakeOptions creates cmdslib `Option`s
// using underlying type and interface data from [*settings].
func MakeOptions[setPtr runtime.SettingsType[settings],
	settings any](options ...ConstructorOption,
) ([]cmds.Option, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fieldParams, errs, err := fieldParamsFromSettings[setPtr](ctx)
	if err != nil {
		return nil, err
	}
	var (
		maybeBuiltin       []cmds.Option
		constructorOptions = parseConstructorOptions(options...)
		userConstructors   = constructorOptions.userConstructors
	)
	if constructorOptions.withBuiltin {
		maybeBuiltin = builtinOptions()
	}

	cmdsOptions := make([]cmds.Option, 0, cap(fieldParams)+len(maybeBuiltin))
	for pairOrErr := range generic.CtxEither(ctx, fieldParams, errs) {
		if err := pairOrErr.Right; err != nil {
			return nil, err
		}
		var (
			fieldAndParam = pairOrErr.Left
			field         = fieldAndParam.Left
			param         = fieldAndParam.Right
			option, err   = newSettingsOption(field, param, userConstructors)
		)
		if err != nil {
			return nil, fmt.Errorf("%T: %w", (setPtr)(nil), err)
		}
		cmdsOptions = append(cmdsOptions, option)
	}
	return append(cmdsOptions, maybeBuiltin...), nil
}

func fieldsFromSettings[setPtr runtime.SettingsType[settings],
	settings any](ctx context.Context,
) (runtime.SettingsFields, <-chan error, error) {
	fields, err := runtime.ReflectFields[setPtr](ctx)
	if err != nil {
		return nil, nil, err
	}
	validFields, errs := checkFields(ctx, fields)
	return validFields, errs, nil
}

func fieldParamsFromSettings[setPtr runtime.SettingsType[settings],
	settings any](ctx context.Context,
) (<-chan fieldParam, <-chan error, error) {
	validFields, errs, err := fieldsFromSettings[setPtr](ctx)
	if err != nil {
		return nil, nil, err
	}

	var (
		params      = setPtr.Parameters(nil, ctx)
		fieldParams = skipEmbbedded(ctx, validFields, params)
	)
	return fieldParams, errs, nil
}

func newSettingsOption(field reflect.StructField,
	param parameters.Parameter, constructors []TypeConstructor,
) (cmds.Option, error) {
	var (
		constructorArgs = parameterToConstructorArgs(param)
		typ             = field.Type
	)
	if userConstructor := maybeGetConstructor(constructors, typ); userConstructor != nil {
		return userConstructor(constructorArgs...), nil
	}
	if builtinConstructor := constructorForKind(typ.Kind()); builtinConstructor != nil {
		return builtinConstructor(constructorArgs...), nil
	}
	err := fmt.Errorf("%w:"+
		" can't determine which option constructor to use for `%s`"+
		" (type %v with no custom handler)",
		runtime.ErrUnexpectedType,
		field.Name,
		typ,
	)
	return nil, err
}

func maybeGetConstructor(constructors []TypeConstructor, typ reflect.Type) NewOptionFunc {
	for _, constructor := range constructors {
		if constructor.Type == typ {
			return constructor.NewOptionFunc
		}
	}
	return nil
}

func parameterToConstructorArgs(parameter parameters.Parameter) []string {
	const nameAndDescription = 2
	var (
		name        = parameter.Name(parameters.CommandLine)
		aliases     = parameter.Aliases(parameters.CommandLine)
		description = fmt.Sprintf("%s (Env: %s)",
			parameter.Description(),
			parameter.Name(parameters.Environment),
		)

		optionCount = len(aliases) + nameAndDescription
		optionArgs  = make([]string, 0, optionCount)
	)

	// NOTE: cmds lib option constructor determines the purpose of these values by their order.
	// Name is first, aliases follow, and the last argument is the description.
	optionArgs = append(optionArgs, name)
	optionArgs = append(optionArgs, aliases...)
	optionArgs = append(optionArgs, description)

	return optionArgs
}
