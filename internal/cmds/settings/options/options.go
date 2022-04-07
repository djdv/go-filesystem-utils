package options

import (
	"context"
	"fmt"
	"reflect"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
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
	optionMakers []OptionMaker

	ConstructorOption  interface{ apply(*cmdsOptionSettings) }
	cmdsOptionSettings struct {
		includeBuiltin bool
		customMakers   optionMakers
	}

	cmdsOptionMakerOpt struct{ OptionMaker }
	cmdsBuiltinOpt     bool
)

func (optionMakers optionMakers) Index(typ reflect.Type) *OptionMaker {
	for _, optionMaker := range optionMakers {
		if optionMaker.Type == typ {
			return &optionMaker
		}
	}
	return nil
}

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

func parseCmdsOptionOptions(options ...ConstructorOption) cmdsOptionSettings {
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
	set any](options ...ConstructorOption) []cmds.Option {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// TODO: Change this to avoid `expandFields` in the first place.
	// Rather than filtering it out.
	// We need to do the binding ourselves too / refactor into function both ops can use
	optionFields, generatorErrs, err := runtime.BindParameterFields[setPtr](ctx)
	if err != nil {
		panic(err)
	}
	reducedFields := onlyRootOptions(ctx, optionFields)

	// TODO: The generator should produce a 3 value struct
	// field, param, paramindex
	// we should allocate for all-opts
	// and blit into it
	// or just append and sort-by-index
	// before calling makeCmdsOption on them
	// (cmds-lib preserves/prints options in the order they're registered)

	var (
		cmdsOptions,
		maybeBuiltin []cmds.Option
		optionBufferHint = cap(reducedFields)

		constructorSettings = parseCmdsOptionOptions(options...)

		makers  = constructorSettings.customMakers
		makeOpt = func(field runtime.ParamField) error {
			opt := makeCmdsOption(field.StructField, field.Parameter, makers)
			cmdsOptions = append(cmdsOptions, opt)
			return nil
		}
	)
	if constructorSettings.includeBuiltin {
		maybeBuiltin = builtinOptions()
		optionBufferHint += len(maybeBuiltin)
	}
	cmdsOptions = make([]cmds.Option, 0, optionBufferHint)
	if err := ForEachOrError(ctx, reducedFields, generatorErrs, makeOpt); err != nil {
		panic(err)
	}

	return append(cmdsOptions, maybeBuiltin...)
}

func builtinOptions() []cmds.Option {
	return []cmds.Option{
		cmds.OptionEncodingType,
		cmds.OptionTimeout,
		cmds.OptionStreamChannels,
		cmds.BoolOption(cmds.OptLongHelp, "Show the full command help text."),
		cmds.BoolOption(cmds.OptShortHelp, "Show a short version of the command help text."),
	}
}

// TODO: when we hit a non-root just exit
// input needs to be canceled when we exit too
// ^ wrap this {ctx;gen;roots;cancel}
func onlyRootOptions(ctx context.Context, params runtime.ParamFields) runtime.ParamFields {
	relay := make(chan runtime.ParamField, cap(params))
	go func() {
		defer close(relay)
		relayRootFields := func(param runtime.ParamField) error {
			if len(param.StructField.Index) > 1 {
				return nil
			}
			select {
			case relay <- param:
			case <-ctx.Done():
				return ctx.Err()
			}
			return nil
		}
		ForEachOrError(ctx, params, nil, relayRootFields)
	}()
	return relay
}

func makeCmdsOption(field reflect.StructField, parameter parameters.Parameter, makers optionMakers) cmds.Option {
	if !field.IsExported() {
		err := fmt.Errorf("%w:"+
			" refusing to create option for unassignable field"+
			" - `%s` is not exported",
			runtime.ErrUnassignable,
			field.Name,
		)
		panic(err)
	}

	var (
		typ        = field.Type
		optionArgs = parameterToCmdsOptionArgs(parameter)
	)
	if customMaker := makers.Index(typ); customMaker != nil {
		return customMaker.MakeOptionFunc(optionArgs...)
	}

	valKind := typ.Kind()
	if builtinMaker := kindToCmdsOptionMaker(valKind); builtinMaker != nil {
		return builtinMaker(optionArgs...)
	}

	err := fmt.Errorf("%w:"+
		" can't determine which option constructor to use for `%s`"+
		" (type %v with no custom handler)",
		runtime.ErrUnexpectedType,
		field.Name,
		typ,
	)
	panic(err)
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

func kindToCmdsOptionMaker(kind reflect.Kind) MakeOptionFunc {
	switch kind {
	case reflect.Bool:
		return cmds.BoolOption
	case reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32:
		return cmds.IntOption
	case reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32:
		return cmds.UintOption
	case reflect.Int64:
		return cmds.Int64Option
	case reflect.Uint64:
		return cmds.Uint64Option
	case reflect.Float32,
		reflect.Float64:
		return cmds.FloatOption
	case reflect.Complex64,
		reflect.Complex128:
		return cmds.StringOption
	case reflect.String:
		return cmds.StringOption
	case reflect.Array,
		reflect.Slice:
		return func(optionArgs ...string) cmds.Option {
			return cmds.DelimitedStringsOption(",", optionArgs...)
		}
	default:
		return nil
	}
}
