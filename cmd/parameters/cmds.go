package parameters

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

// CmdsOptionsFrom creates a list of cmds-lib options from a Settings interface.
// It is expected to be called only during process initialization
// and will panic if the provided type does not conform to the expectations of this library.
// (The error message should explain why / what part of the interface did not meet expectations)
func CmdsOptionsFrom(settings Settings) (cmdsOptions []cmds.Option) {
	settingsType, settingsTypeErr := checkTypeFor(settings)
	if settingsTypeErr != nil {
		panic(settingsTypeErr)
	}
	argumentField, settingsDeclareErr := argumentFieldIn(settingsType)
	if settingsDeclareErr != nil {
		panic(settingsDeclareErr)
	}

	var (
		isEmbedded     = len(argumentField.Index) > 1
		parameters     = settings.Parameters()
		parameterCount = len(parameters)
		options        = cmdsOptionsFrom(settingsType, argumentField.Index, parameters)
		optionsBound   = len(options)
	)
	if isEmbedded {
		// We won't generate these options, so we won't count them either.
		var (
			argsEnd         = len(argumentField.Index) - 1
			containerIndex  = argumentField.Index[:argsEnd]
			containerOffset = argumentField.Index[argsEnd]
			containerCount  = settingsType.FieldByIndex(containerIndex).Type.NumField() - containerOffset
		)
		parameterCount -= containerCount
	}
	if optionsBound < parameterCount {
		remainder := parameters[optionsBound:]
		err := fmt.Errorf(
			"%s doesn't have enough fields declared after settings tag"+
				" - constructed %d options, need %d fields to fit remaining parameters: [%s]",
			settingsType.Name(),
			optionsBound, parameterCount,
			remainder,
		)
		panic(err)
	}

	return options
}

func cmdsOptionsFrom(settingsType reflect.Type, settingsIndex []int, parameters Parameters) []cmds.Option {
	var (
		optionsBound   int
		parameterCount = len(parameters)
		cmdsOptions    = make([]cmds.Option, 0, parameterCount)
		ctx, cancel    = context.WithCancel(context.Background())
		fieldOffset    = settingsIndex[0]
		fields         = fieldsFrom(ctx, settingsType, fieldOffset)
		// Embedded parameter options
		// should already be registered by their super-cmd,
		// and may not be registered with the cmds-lib again.
		// As a result, we must skip embedded settings types
		// and produce no (duplicate) cmds.Options in our returned slice.
		tagTypeIndex     = settingsIndex[:len(settingsIndex)-1]
		taggedFieldIndex = settingsIndex[len(settingsIndex)-1]
		taggedType       = settingsType.FieldByIndex(tagTypeIndex).Type
	)
	defer cancel()
	for field := range fields {
		if optionsBound == parameterCount {
			break
		}
		if field.Type.Kind() == reflect.Struct &&
			field.Anonymous {
			if field.Type == taggedType {
				// Treat fields in the tagged type
				// as already bound, and skip processing them.
				optionsBound += taggedType.NumField() - taggedFieldIndex
				continue
			}
			// All other embedded structs get expanded into their fields recursively,
			// and processed into cmds.Options.
			var (
				subParameters   = parameters[optionsBound:]
				embeddedOptions = cmdsOptionsFrom(field.Type, []int{0}, subParameters)
			)
			cmdsOptions = append(cmdsOptions, embeddedOptions...)
			optionsBound += len(embeddedOptions)
			continue
		}

		// Inspect argument type to discern what cmds.Option type to use,
		// and bind the parameter data to it.
		cmdsOption := toCmdsOption(field, parameters[optionsBound])
		cmdsOptions = append(cmdsOptions, cmdsOption)
		optionsBound++
	}

	return cmdsOptions
}

// SettingsFromCmds uses a cmds.Request as a source for settings values.
func SettingsFromCmds(request *cmds.Request) SettingsSource {
	return cmdsSettingsSource{Request: request}
}

type cmdsSettingsSource struct{ Request *cmds.Request }

func (cs cmdsSettingsSource) setEach(ctx context.Context,
	argsToSet ArgumentList,
	inputErrors <-chan error) (ArgumentList, <-chan error) {
	options := cs.Request.Options
	if !hasUserDefinedOptions(options) {
		// If the request only contains cmds-lib specific values,
		// we skip processing entirely.
		// Relaying our inputs as our outputs.
		return argsToSet, inputErrors
	}
	var (
		unsetArgs = make(chan *Argument, cap(argsToSet))
		errors    = make(chan error, cap(inputErrors))
	)
	go func() {
		defer close(unsetArgs)
		defer close(errors)
		for argsToSet != nil ||
			inputErrors != nil {
			select {
			case argument, ok := <-argsToSet:
				if !ok {
					argsToSet = nil
					continue
				}
				// NOTE: We don't check any name aliases.
				// This is the responsibility of `Request.SetOption`, not us.
				commandlineName := argument.Parameter.CommandLine()
				cmdsArg, provided := options[commandlineName]
				if provided {
					if err := assignToArgument(argument, cmdsArg); err != nil {
						select {
						case errors <- fmt.Errorf(
							"parameter `%s`: couldn't assign value: %w",
							commandlineName, err):
						case <-ctx.Done():
						}
					}
					continue
				}
				select { // Relay parameter to next source.
				case unsetArgs <- argument:
				case <-ctx.Done():
				}
			case err, ok := <-inputErrors:
				if !ok {
					inputErrors = nil
					continue
				}
				// If we encounter an input error,
				// relay it and keep processing.
				// The caller may decide to cancel or not afterwards.
				select {
				case errors <- err:
				case <-ctx.Done():
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return unsetArgs, errors
}

func hasUserDefinedOptions(options cmds.OptMap) bool {
	var (
		hasUserOptions bool
		cmdsExclusive  = [...]string{
			cmds.EncLong,
			cmds.RecLong,
			cmds.ChanOpt,
			cmds.TimeoutOpt,
			cmds.DerefLong,
			cmds.StdinName,
			cmds.Hidden,
			cmds.Ignore,
			cmds.IgnoreRules,
			cmds.OptLongHelp,
			cmds.OptShortHelp,
		}
	)
optChecker:
	for optName := range options {
		for _, cmdsName := range cmdsExclusive {
			if optName == cmdsName {
				continue optChecker
			}
		}
		hasUserOptions = true
		break
	}
	return hasUserOptions
}

func toCmdsOption(field reflect.StructField, parameter Parameter) cmds.Option {
	var (
		optionConstructor func(...string) cmds.Option

		// time.Duration gets a special case.
		// (Its Kind overlaps with int64)
		// We also prefer input to be in string format.
		// `param=3s` not `param=3000000000`.
		durationType = reflect.TypeOf((*time.Duration)(nil)).Elem()
		// Simple strings <-> enum types
		fsIDType   = reflect.TypeOf((*filesystem.ID)(nil)).Elem()
		hostIDType = reflect.TypeOf((*filesystem.API)(nil)).Elem()
	)
	// TODO: When Go 1.17 is released
	// if !field.IsExported() {
	if field.PkgPath != "" {
		panic(fmt.Errorf(
			"field `%s` is not exported and thus not settable - refusing to create option",
			field.Name),
		)
	}

	// Special type cases
	switch field.Type {
	case durationType,
		fsIDType,
		hostIDType:
		optionConstructor = cmds.StringOption
		goto ret
	}

	// Generic types.
	switch optionKind := field.Type.Kind(); optionKind {
	case cmds.Bool:
		optionConstructor = cmds.BoolOption
	case cmds.Int:
		optionConstructor = cmds.IntOption
	case cmds.Uint:
		optionConstructor = cmds.UintOption
	case cmds.Int64:
		optionConstructor = cmds.Int64Option
	case cmds.Uint64:
		optionConstructor = cmds.Uint64Option
	case cmds.Float:
		optionConstructor = cmds.FloatOption
	case cmds.String:
		optionConstructor = cmds.StringOption
	case cmds.Strings,
		reflect.Slice:
		optionConstructor = func(names ...string) cmds.Option {
			return cmds.DelimitedStringsOption(",", names...)
		}
	case reflect.Interface:
		maddrType := reflect.TypeOf((*multiaddr.Multiaddr)(nil)).Elem()
		if field.Type.Implements(maddrType) {
			optionConstructor = cmds.StringOption
			break
		}
		fallthrough
	default:
		typeErr := fmt.Errorf(
			"Can't determine which option to use for parameter `%s` (type: %s Kind: %s)",
			parameter.CommandLine(),
			field.Type, optionKind,
		)
		panic(typeErr)
	}

ret:
	var (
		name        = parameter.CommandLine()
		aliases     = parameter.CommandLineAliases()
		description = fmt.Sprintf("%s (Env: %s)",
			parameter.Description(),
			parameter.Environment(),
		)
		optionCount = 2 + len(aliases) // 2: name+description.
		optionSlice = make([]string, 0, optionCount)
	)

	// NOTE: cmds lib option constructor determines the purpose of these values by their order.
	// Name is first, aliases follow, and the last argument is the description.
	optionSlice = append(optionSlice, name)
	optionSlice = append(optionSlice, aliases...)
	optionSlice = append(optionSlice, description)

	return optionConstructor(optionSlice...)
}
