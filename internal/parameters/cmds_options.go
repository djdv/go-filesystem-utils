package parameters

import (
	"context"
	"fmt"
	"reflect"

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

	// CmdsOptionOption is a funny name used for the option constructor's own options.
	CmdsOptionOption   interface{ apply(*cmdsOptionSettings) }
	cmdsOptionSettings struct {
		includeBuiltin bool
		customMakers   optionMakers
	}

	cmdsOptionMakerOpt struct{ OptionMaker }
	cmdsBuiltinOpt     bool

	optionField struct {
		Parameter
		reflect.StructField
	}
	optionFields <-chan optionField
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
func WithBuiltin(b bool) CmdsOptionOption              { return cmdsBuiltinOpt(b) }
func (b cmdsBuiltinOpt) apply(set *cmdsOptionSettings) { set.includeBuiltin = bool(b) }

// WithMaker supplies the Settings parser
// with a constructor for a non-built-in type.
// (This option may be provided multiple times for multiple types.)
func WithMaker(maker OptionMaker) CmdsOptionOption { return cmdsOptionMakerOpt{maker} }

func (maker cmdsOptionMakerOpt) apply(set *cmdsOptionSettings) {
	set.customMakers = append(set.customMakers, maker.OptionMaker)
}

func parseCmdsOptionOptions(options ...CmdsOptionOption) cmdsOptionSettings {
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
// In order to support subcommands/subsettings, this function
// skips any embedded (assumed super-)settings structs.
// (The expectation is that a parent command has already registered them.)
func MustMakeCmdsOptions(set Settings, options ...CmdsOptionOption) []cmds.Option {
	var (
		parameters  = set.Parameters()
		ctx, cancel = context.WithCancel(context.Background())
	)
	defer cancel()

	optionFields, generatorErrs, err := optionsFromSettings(ctx, set)
	if err != nil {
		panic(err)
	}

	var (
		maybeBuiltin        []cmds.Option
		constructorSettings = parseCmdsOptionOptions(options...)
	)
	if constructorSettings.includeBuiltin {
		maybeBuiltin = builtinOptions()
	}

	var (
		parameterCount = len(parameters)
		cmdsOptions    = make([]cmds.Option, 0, parameterCount+len(maybeBuiltin))
	)
	if parameterCount != 0 {
		var (
			makers  = constructorSettings.customMakers
			makeOpt = func(field optionField) error {
				cmdsOptions = append(cmdsOptions,
					makeCmdsOption(field.StructField, field.Parameter, makers))
				return nil
			}
		)
		if err := forEachOrError(ctx, optionFields, generatorErrs, makeOpt); err != nil {
			panic(err)
		}
	}

	cmdsOptions = append(cmdsOptions, maybeBuiltin...)
	return cmdsOptions
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

func optionsFromSettings(ctx context.Context, set Settings) (optionFields, errorCh, error) {
	typ, err := checkType(set)
	if err != nil {
		return nil, nil, err
	}
	fields, fieldsErrs := generateOptionFields(ctx, typ, set.Parameters())
	return fields, fieldsErrs, nil
}

func generateOptionFields(ctx context.Context,
	typ reflect.Type, parameters Parameters,
) (optionFields, errorCh) {
	subCtx, cancel := context.WithCancel(ctx)
	var (
		baseFields = generateFields(subCtx, typ)
		allFields  = expandFields(subCtx, baseFields)

		tag                   = newStructTagPair(settingsTagKey, settingsTagValue)
		taggedFields, tagErrs = fieldsAfterTag(subCtx, tag, allFields)

		paramCount    = len(parameters)
		reducedFields = ctxTakeAndCancel(subCtx, cancel, paramCount, taggedFields)

		results = make(chan optionField, cap(reducedFields))
		errs    = make(chan error)
	)
	go func() {
		defer close(results)
		defer close(errs)
		var (
			parameterIndex int
			relay          = func(field reflect.StructField) error {
				fieldsBuf, skipped, err := skipEmbeddedOptions(field, reducedFields, tag)
				if err != nil {
					return err
				}
				parameterIndex += skipped
				for _, field := range fieldsBuf {
					opt := optionField{
						Parameter:   parameters[parameterIndex],
						StructField: field,
					}
					select {
					case results <- opt:
					case <-ctx.Done():
						return ctx.Err()
					}
					parameterIndex++
				}
				return nil
			}
			maybeSendErr = func(err error) {
				if err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
					}
				}
			}
		)
		maybeSendErr(forEachOrError(ctx, reducedFields, tagErrs, relay))
		if ctx.Err() == nil { // Don't validate if we're canceled.
			expected := len(parameters)
			maybeSendErr(checkParameterCount(parameterIndex, expected, typ, parameters))
		}
	}()

	return results, errs
}

func skipEmbeddedOptions(field reflect.StructField,
	fields structFields, tag structTagPair,
) ([]reflect.StructField, int, error) {
	structDepth := len(field.Index)
	if isEmbedded := structDepth > 1; !isEmbedded {
		return []reflect.StructField{field}, 0, nil
	}
	var (
		sawTag        bool
		skipped       int
		fieldsCap     = cap(fields)
		arbitrarySize = fieldsCap / 2 // We'll trade memory for allocs.
		fieldBuffer   = make([]reflect.StructField, 0, arbitrarySize)
	)
	for ok := true; ok; field, ok = <-fields {
		if climbedUp := len(field.Index) == 1; climbedUp {
			fieldBuffer = append(fieldBuffer, field)
			break
		}
		skipped++
		if !sawTag {
			var err error
			if sawTag, err = hasTagValue(field, tag); err != nil {
				return nil, 0, err
			}
			if sawTag {
				fieldBuffer = nil
				continue
			}
			fieldBuffer = append(fieldBuffer, field)
		}
	}

	if !sawTag {
		skipped = 0
	}

	return fieldBuffer, skipped, nil
}

func makeCmdsOption(field reflect.StructField, parameter Parameter, makers optionMakers) cmds.Option {
	if !field.IsExported() {
		err := fmt.Errorf("%w:"+
			" refusing to create option for unassignable field"+
			" - `%s` is not exported",
			errUnassignable,
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
		errUnexpectedType,
		field.Name,
		typ,
	)
	panic(err)
}

func parameterToCmdsOptionArgs(parameter Parameter) []string {
	const nameAndDescription = 2
	var (
		name        = parameter.Name(CommandLine)
		aliases     = parameter.Aliases(CommandLine)
		description = fmt.Sprintf("%s (Env: %s)",
			parameter.Description(),
			parameter.Name(Environment),
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
	case cmds.Bool:
		return cmds.BoolOption
	case cmds.Int:
		return cmds.IntOption
	case cmds.Uint:
		return cmds.UintOption
	case cmds.Int64:
		return cmds.Int64Option
	case cmds.Uint64:
		return cmds.Uint64Option
	case cmds.Float:
		return cmds.FloatOption
	case cmds.String:
		return cmds.StringOption
	case cmds.Strings,
		reflect.Slice:
		return func(optionArgs ...string) cmds.Option {
			return cmds.DelimitedStringsOption(",", optionArgs...)
		}
	default:
		return nil
	}
}
