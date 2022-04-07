package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type (
	// TODO: name
	SettingsConstraint[settings any] interface {
		*settings
		parameters.Settings
	}

	CmdsParameter struct {
		Namespace,
		OptionName,
		HelpText,
		envPrefix string // TODO: can we eliminate this field?
		OptionAliases []string
	}
)

func (parameter CmdsParameter) Description() string { return parameter.HelpText }
func (parameter CmdsParameter) Name(source parameters.SourceID) string {
	switch source {
	case parameters.CommandLine:
		return cliName(parameter.OptionName)
	case parameters.Environment:
		return envName(parameter.envPrefix, parameter.Namespace, parameter.OptionName)
	default:
		err := fmt.Errorf("%w: %v", parameters.ErrUnexpectedSourceID, source)
		panic(err)
	}
}

func (parameter CmdsParameter) Aliases(source parameters.SourceID) []string {
	aliases := make([]string, 0, len(parameter.OptionAliases))
	switch source {
	case parameters.CommandLine:
		for _, name := range parameter.OptionAliases {
			aliases = append(aliases, cliName(name))
		}
		return aliases
	case parameters.Environment:
		var (
			prefix    = parameter.envPrefix
			namespace = parameter.Namespace
		)
		for _, name := range parameter.OptionAliases {
			aliases = append(aliases, envName(prefix, namespace, name))
		}
		return aliases
	default:
		err := fmt.Errorf("%w: %v", parameters.ErrUnexpectedSourceID, source)
		panic(err)
	}
}

// TODO: outdated? check comment
/// Parse will populate `set` using values returned by each `SetFunc`.
// Value sources are queried in the same order they're provided.
// If a setting cannot be set by one source,
// it's reference is relayed to the next `SetFunc`.
func Parse[setIntf SettingsConstraint[set], set any](ctx context.Context,
	setFuncs []SetFunc, parsers ...TypeParser,
) (*set, error) {
	settingsPointer := new(set)
	unsetArgs, generatorErrs, err := ArgsFromSettings[setIntf](ctx, settingsPointer)
	if err != nil {
		return nil, err
	}

	const generatorChanCount = 1
	errChans := make([]<-chan error, 0,
		generatorChanCount+len(setFuncs),
	)
	errChans = append(errChans, generatorErrs)

	for _, setter := range setFuncs {
		var errChan <-chan error
		unsetArgs, errChan = setter(ctx, unsetArgs, parsers...)
		errChans = append(errChans, errChan)
	}

	var (
		errs  = generic.CtxMerge(ctx, errChans...)
		drain = func(Argument) error { return nil }
	)
	if err := generic.ForEachOrError(ctx, unsetArgs, errs, drain); err != nil {
		return nil, fmt.Errorf("Parse encountered an error: %w", err)
	}
	return settingsPointer, ctx.Err()
}

/* TODO re-do docs (things changed)
// NewParameter constructs a parameter using either the provided options,
// or a set of defaults (derived from the calling function's name, pkg, and binary name).
*/
func MustMakeParameters[setPtr SettingsConstraint[settings], settings any](ctx context.Context,
	partialParams []CmdsParameter,
) parameters.Parameters {
	typ, err := checkType[settings]()
	if err != nil {
		panic(err)
	}
	paramCount := len(partialParams)
	if typ.NumField() < paramCount {
		panic(errTooFewFields)
	}

	var (
		params               = make(chan parameters.Parameter, paramCount)
		namespace, envPrefix = programMetadata()
	)
	go func() {
		defer close(params)
		subCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		fields := generateFields(subCtx, typ)
		for _, param := range partialParams {
			select {
			case field, ok := <-fields:
				if !ok {
					return
				}
				fieldName := field.Name
				if param.OptionName == "" {
					param.OptionName = fieldName
				}
				if param.HelpText == "" {
					param.HelpText = fmt.Sprintf(
						"Dynamic parameter for %s",
						fieldName,
					)
				}
				if param.Namespace == "" &&
					namespace != "" {
					param.Namespace = namespace
				}
				param.envPrefix = envPrefix
				select {
				case params <- param:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return params
}

func programMetadata() (namespace, envPrefix string) {
	funcLocation, _, _, ok := runtime.Caller(2)
	if !ok {
		panic("runtime could not get program counter address for function")
	}
	namespace, _ = funcNames(funcLocation)
	// TODO: Not a great solution.
	// Rather than filtering ourselves, we need a way for the caller to tell us
	// to just not use a namespace.
	if namespace == "settings" {
		namespace = ""
	}

	progName := filepath.Base(os.Args[0])
	envPrefix = strings.TrimSuffix(progName, filepath.Ext(progName))
	return
}
