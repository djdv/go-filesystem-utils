package parameters

import (
	"context"
	"fmt"
	"reflect"

	. "github.com/djdv/go-filesystem-utils/internal/generic"
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	optionFields, generatorErrs, err := cmdsOptionsFromSettings(ctx, set)
	if err != nil {
		panic(err)
	}

	// TODO: The generator should produce a 3 value struct
	// field, param, paramindex
	// we should allocate for all-opts
	// and blit into it
	// or just append and sort-by-index
	// before calling makeCmdsOption on them
	// (cmds-lib preserves options as they're registered)

	var (
		cmdsOptions,
		maybeBuiltin []cmds.Option
		optionBufferHint = cap(optionFields)

		constructorSettings = parseCmdsOptionOptions(options...)

		makers  = constructorSettings.customMakers
		makeOpt = func(field paramField) error {
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
	if err := ForEachOrError(ctx, optionFields, generatorErrs, makeOpt); err != nil {
		panic(err)
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

func cmdsOptionsFromSettings(ctx context.Context, set Settings) (paramFields, errCh, error) {
	typ, err := checkType(set)
	if err != nil {
		return nil, nil, err
	}
	var (
		parameters              = set.Parameters()
		optionFields, paramErrs = bindParameterFields(ctx, typ, parameters)

		partitions = partitionOptionFields(ctx, optionFields)

		tag                       = newStructTagPair(settingsTagKey, settingsTagValue)
		reducedFields, reduceErrs = skipEmbeddedOptions(ctx, tag, partitions)
		errs                      = CtxMerge(ctx, paramErrs, reduceErrs)
	)
	return reducedFields, errs, nil
}

func partitionOptionFields(ctx context.Context, fields paramFields) paramBridge {
	fieldPartitions := make(chan paramFields)
	go func() {
		defer close(fieldPartitions)
		var (
			lastIndex      int
			isStructBranch = func(field paramField) bool {
				var (
					currentIndex  = field.Index[0]
					structChanged = currentIndex != lastIndex
				)
				lastIndex = currentIndex
				return structChanged
			}
		)
		segmentInput(ctx, fields, fieldPartitions, isStructBranch)
	}()

	return fieldPartitions
}

func segmentInput[in any](ctx context.Context,
	input <-chan in,
	bridge chan (<-chan in),
	isNewSegment func(in) bool,
) {
	var relay chan in
	for element := range input {
		if isNewSegment(element) {
			if relay != nil {
				close(relay)
				relay = nil
			}
		}
		if relay == nil {
			relay = make(chan in)
			select {
			case bridge <- relay:
			case <-ctx.Done():
				break
			}
		}
		select {
		case relay <- element:
		case <-ctx.Done():
			break
		}
	}
	if relay != nil {
		close(relay)
	}
}

func skipEmbeddedOptions(ctx context.Context,
	optionTagToSkip structTagPair,
	segmentedParams paramBridge,
) (paramFields, errCh) {
	var (
		out  = make(chan paramField, cap(segmentedParams))
		errs = make(chan error)
	)
	go func() {
		defer close(out)
		defer close(errs)
		for params := range segmentedParams {
			maybeFields, tErrs := skipEmbeddedOptionFields(ctx, optionTagToSkip, params)
			fn := func(param paramField) error {
				select {
				case out <- param:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			if err := ForEachOrError(ctx, maybeFields, tErrs, fn); err != nil {
				select {
				case errs <- err:
				case <-ctx.Done():
				}
			}
		}
	}()
	return out, errs
}

func skipEmbeddedOptionFields(ctx context.Context,
	optionTagToSkip structTagPair,
	fields paramFields,
) (paramFields, errCh) {
	var (
		out  = make(chan paramField, cap(fields))
		errs = make(chan error)
	)
	go func() {
		defer close(out)
		defer close(errs)
		var (
			fieldBuffer = make([]paramField, 0, cap(fields))
			sawTag      bool
			checkTags   = func(param paramField) error {
				if sawTag {
					return nil
				}
				var (
					field            = param.StructField
					fieldStructDepth = len(field.Index)
					fieldIsEmbedded  = fieldStructDepth > 1
				)
				if fieldIsEmbedded {
					var err error
					if sawTag, err = hasTagValue(field, optionTagToSkip); err != nil {
						return err
					}
					if sawTag {
						return nil
					}
				}
				fieldBuffer = append(fieldBuffer, param)
				return nil
			}
		)
		if err := ForEachOrError(ctx, fields, nil, checkTags); err != nil {
			select {
			case errs <- err:
			case <-ctx.Done():
			}
		}
		if sawTag {
			return
		}
		for _, field := range fieldBuffer {
			if ctx.Err() != nil {
				return
			}
			out <- field
		}
	}()
	return out, errs
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
