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

	// OptionMaker is the binding of a type with its corresponding cmds.Option constructor.
	OptionMaker struct {
		reflect.Type
		MakeOptionFunc
	}

	ConstructorOption  interface{ apply(*cmdsOptionSettings) }
	cmdsOptionSettings struct {
		customMakers   []OptionMaker
		includeBuiltin bool
	}

	cmdsOptionMakerOpt struct{ OptionMaker }
	cmdsBuiltinOpt     bool
)

// WithBuiltin includes the cmds-lib native options (such as `--help`, `--timeout`, and more)
// in the returned options.
func WithBuiltin(b bool) ConstructorOption             { return cmdsBuiltinOpt(b) }
func (b cmdsBuiltinOpt) apply(set *cmdsOptionSettings) { set.includeBuiltin = bool(b) }

// WithMaker supplies the Settings parser
// with a constructor for a non-built-in type.
// (This option may be provided multiple times for multiple types.)
func WithMaker(maker OptionMaker) ConstructorOption { return cmdsOptionMakerOpt{maker} }

func (maker cmdsOptionMakerOpt) apply(set *cmdsOptionSettings) {
	set.customMakers = append(set.customMakers, maker.OptionMaker)
}

func parseConstructorOptions(options ...ConstructorOption) cmdsOptionSettings {
	var set cmdsOptionSettings
	for _, opt := range options {
		opt.apply(&set)
	}
	return set
}

// MustMakeCmdsOptions creates cmds-lib options from a Settings interface.
// It is expected to be called only during process initialization
// and will panic if the provided type does not conform to the expectations of this library.
//
// NOTE: The cmds-lib panics when registering duplicate options.
// In order to support subcommands, this function
// skips any embedded (assumed super-)settings structs.
// (The expectation is that a parent command has already registered them.)
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
		params                       = setPtr.Parameters(nil, ctx)
		reducedFields, reducedParams = skipEmbbedded(ctx, fields, params)
		fieldParams, errs            = generic.CtxPair(ctx, reducedFields, reducedParams)
		optionBufferHint             = cap(fieldParams)
		cmdsOptions,
		maybeBuiltin []cmds.Option

		constructorSettings = parseConstructorOptions(options...)
		makers              = constructorSettings.customMakers
	)
	if constructorSettings.includeBuiltin {
		maybeBuiltin = builtinOptions()
		optionBufferHint += len(maybeBuiltin)
	}
	cmdsOptions = make([]cmds.Option, 0, optionBufferHint)

	for result := range generic.CtxEither(ctx, fieldParams, errs) {
		if err := result.Right; err != nil {
			typ := reflect.TypeOf((*set)(nil)).Elem()
			panic(fmt.Errorf("%s: %w", typ, err))
		}
		var (
			pair     = result.Left
			opt, err = makeCmdsOption(pair.Left, pair.Right, makers)
		)
		if err != nil {
			typ := reflect.TypeOf((*set)(nil)).Elem()
			panic(fmt.Errorf("%s: %w", typ, err))
		}
		cmdsOptions = append(cmdsOptions, opt)
	}

	return append(cmdsOptions, maybeBuiltin...)
}

func skipEmbbedded(ctx context.Context, fields runtime.StructFields,
	params parameters.Parameters,
) (runtime.StructFields, parameters.Parameters) {
	var (
		reducedFields = make(chan reflect.StructField, cap(fields))
		reducedParams = make(chan parameters.Parameter, cap(params))
	)
	go func() {
		defer close(reducedFields)
		defer close(reducedParams)
		for field := range fields {
			if field.Anonymous &&
				field.Type.Kind() == reflect.Struct {
				for skipCount := field.Type.NumField(); skipCount != 0; skipCount-- {
					select {
					case <-params:
					case <-ctx.Done():
						return
					}
				}
				continue
			}
			// TODO: can we simplify this?
			var param parameters.Parameter
			select {
			case param = <-params:
			case <-ctx.Done():
				return
			}
			select {
			case reducedFields <- field:
				select {
				case reducedParams <- param:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return reducedFields, reducedParams
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
	if builtinMaker := kindToCmdsOptionMaker(valKind); builtinMaker != nil {
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
