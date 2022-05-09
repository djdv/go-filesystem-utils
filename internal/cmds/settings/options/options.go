// Package options provides generic constructors for the cmds-lib Option interface.
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
	// MakeOptionFunc should follow the cmds-lib option constructors convention.
	//
	// I.e. the first argument is the primary name (e.g. `some name` => `--some-name`),
	// additional arguments are aliases (`n` => `-n`),
	// and the final argument is the description for the option (used in user facing help text).
	MakeOptionFunc func(...string) cmds.Option

	// OptionMaker is the binding of a type with its corresponding `cmds.Option` constructor.
	OptionMaker struct {
		reflect.Type
		MakeOptionFunc
	}

	// ConstructorOption is the functional options interface for `[]cmds.Option` constructors.
	ConstructorOption  interface{ apply(*cmdsOptionSettings) }
	cmdsOptionSettings struct {
		customMakers []OptionMaker
		withBuiltin  bool
	}

	cmdsOptionMakerOpt struct{ OptionMaker }
	cmdsBuiltinOpt     bool

	fieldParam = generic.Tuple[reflect.StructField, parameters.Parameter]
)

// WithBuiltin sets whether to cmds-lib native options
// (such as `--help`, `--timeout`, and more) should be constructed.
func WithBuiltin(b bool) ConstructorOption             { return cmdsBuiltinOpt(b) }
func (b cmdsBuiltinOpt) apply(set *cmdsOptionSettings) { set.withBuiltin = bool(b) }

// WithMaker appends the OptionMaker to an internal handler list.
func WithMaker(maker OptionMaker) ConstructorOption { return cmdsOptionMakerOpt{maker} }

func (maker cmdsOptionMakerOpt) apply(set *cmdsOptionSettings) {
	set.customMakers = append(set.customMakers, maker.OptionMaker)
}

// MustMakeCmdsOptions creates cmds-lib options from a Settings struct's fields.
// Skipping any embedded sructs.
func MustMakeCmdsOptions[setPtr runtime.SettingsConstraint[set],
	set any](options ...ConstructorOption,
) []cmds.Option {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fields, err := runtime.ReflectFields[setPtr](ctx)
	if err != nil {
		panic(err)
	}
	var (
		params            = setPtr.Parameters(nil, ctx)
		fieldParams, errs = skipEmbbedded(ctx, fields, params)

		settings = parseConstructorOptions(options...)
		makers   = settings.customMakers

		paramCount                = cap(params)
		cmdsOptions, maybeBuiltin = newOptionsSlices(paramCount, settings.withBuiltin)

		panicWith = func(err error) { panic(fmt.Errorf("%T: %w", (setPtr)(nil), err)) }
	)
	for result := range generic.CtxEither(ctx, fieldParams, errs) {
		if err := result.Right; err != nil {
			panicWith(err)
		}
		var (
			pair     = result.Left
			opt, err = makeCmdsOption(pair.Left, pair.Right, makers)
		)
		if err != nil {
			panicWith(err)
		}
		cmdsOptions = append(cmdsOptions, opt)
	}

	return append(cmdsOptions, maybeBuiltin...)
}

func parseConstructorOptions(options ...ConstructorOption) (set cmdsOptionSettings) {
	for _, opt := range options {
		opt.apply(&set)
	}
	return
}

func newOptionsSlices(userOpts int, withBuiltin bool) (cmdsOptions, builtin []cmds.Option) {
	if withBuiltin {
		var (
			builtin     = builtinOptions()
			cmdsOptions = make([]cmds.Option, 0, userOpts+len(builtin))
		)
		return cmdsOptions, builtin
	}
	return make([]cmds.Option, 0, userOpts), nil
}

func skipEmbbedded(ctx context.Context, fields runtime.StructFields,
	params parameters.Parameters,
) (<-chan fieldParam, <-chan error) {
	var (
		fieldParams = make(chan fieldParam, cap(fields)+cap(params))
		errs        = make(chan error)
	)
	go func() {
		defer close(fieldParams)
		defer close(errs)
		for field := range fields {
			if isEmbeddedField(field) {
				skipStruct(ctx, field, params)
				continue
			}
			select {
			case param, ok := <-params:
				if !ok {
					return
				}
				select {
				case fieldParams <- fieldParam{Left: field, Right: param}:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return fieldParams, errs
}

func isEmbeddedField(field reflect.StructField) bool {
	return field.Anonymous && field.Type.Kind() == reflect.Struct
}

func skipStruct(ctx context.Context, field reflect.StructField, params parameters.Parameters) {
	for skipCount := field.Type.NumField(); skipCount != 0; skipCount-- {
		select {
		case <-params:
		case <-ctx.Done():
			return
		}
	}
}

func makeCmdsOption(field reflect.StructField,
	param parameters.Parameter, makers []OptionMaker,
) (cmds.Option, error) {
	if !field.IsExported() {
		err := fmt.Errorf("%w:"+
			" refusing to create option for unassignable field"+
			" - `%s` is not exported",
			runtime.ErrUnassignable,
			field.Name,
		)
		return nil, err
	}

	var (
		typ        = field.Type
		optionArgs = parameterToCmdsOptionArgs(param)
	)
	if customMaker := maybeGetMaker(makers, typ); customMaker != nil {
		return customMaker.MakeOptionFunc(optionArgs...), nil
	}

	valKind := typ.Kind()
	if builtinMaker := constructorForKind(valKind); builtinMaker != nil {
		return builtinMaker(optionArgs...), nil
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

func maybeGetMaker(optionMakers []OptionMaker, typ reflect.Type) *OptionMaker {
	for _, optionMaker := range optionMakers {
		if optionMaker.Type == typ {
			return &optionMaker
		}
	}
	return nil
}

func parameterToCmdsOptionArgs(parameter parameters.Parameter) []string {
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
