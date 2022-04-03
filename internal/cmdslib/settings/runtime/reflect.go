package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type (
	// TODO: name
	SettingsConstraint[settingsPtr any] interface {
		parameters.Settings
		*settingsPtr
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
func Parse[set any, setIntf SettingsConstraint[set]](ctx context.Context,
	setFuncs []SetFunc, parsers ...TypeParser,
) (*set, error) {
	settingsPointer := new(set)
	unsetArgs, generatorErrs, err := ArgsFromSettings[set, setIntf](ctx, settingsPointer)
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
		errs  = CtxMerge(ctx, errChans...)
		drain = func(Argument) error { return nil }
	)
	if err := ForEachOrError(ctx, unsetArgs, errs, drain); err != nil {
		return nil, fmt.Errorf("Parse encountered an error: %w", err)
	}
	return settingsPointer, ctx.Err()
}

// Either, take in a callback, or return a channel.
// ^ do the latter, use ForEach within params?
// TODO: replace ParameterMaker with this.
//func GenerateParameters[in any](fn func(reflect.StructField) Parameter.Parameters) parameters.Parameters {
//func GenerateParameters[in any](ctx context.Context) StructFields {
//
// TODO: needs to take in at least a `descriptions [$len(fields)]string`
// Either take in things like namespace and return params
// or return chan of partial params for caller to range over and complete.
func GenerateParameters[settings any, setPtr SettingsConstraint[settings]](ctx context.Context,
	partialParams []CmdsParameter,
) parameters.Parameters {
	/* TODO re-use docs
	// NewParameter constructs a parameter using either the provided options,
	// or a set of defaults (derived from the calling function's name, pkg, and binary name).
	*/
	typ, err := checkType[settings, setPtr]()
	if err != nil {
		panic(err)
	}

	// Meta ^^ extract?

	// FIXME:
	// should we just do root params? (generate only, no expand)
	// caller can combine sub-structs manually with CtxJoin.
	// Our loop swaps target? range over params, pull from fields?
	// or just keep the same.
	// ^ we should invert, there's no reason for us to skip embedded structs
	// we should just stop processing before we hit it
	// (if the partials are the right length, if not fail somewhere later in the pipeline)
	params := make(chan parameters.Parameter, len(partialParams))
	go func() {
		defer close(params)
		var (
			fields               = generateFields(ctx, typ)
			namespace, envPrefix = programMetadata()
		)
		for _, param := range partialParams {
			select {
			case field, ok := <-fields:
				if !ok {
					// log.Println("fields closed")
					return
				}
				fieldName := field.Name
				// log.Println("got field:", fieldName)
				if param.OptionName == "" {
					param.OptionName = fieldName
				}
				if param.HelpText == "" {
					param.HelpText = fmt.Sprintf( // TODO: from descs
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
					// log.Println("sent param from field:", fieldName)
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
	if namespace, _ = funcNames(funcLocation); namespace == "main" {
		namespace = ""
	}
	progName := filepath.Base(os.Args[0])
	envPrefix = strings.TrimSuffix(progName, filepath.Ext(progName))
	return
}

/*
func PrependParameters(ctx context.Context,
	header []CmdsParameter, tails ...parameters.Parameters) parameters.Parameters {
	relay := make(chan parameters.Parameter, func() (total int) {
		total += len(header)
		for _, ch := range tails {
			total += cap(ch)
		}
		return
	}())
	go func() {
		defer close(relay)
		for _, param := range header {
			select {
			case relay <- param:
			case <-ctx.Done():
				return
			}
		}
		for _, source := range tails {
			for param := range ctxRange(ctx, source) {
				select {
				case relay <- param:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return relay
}
*/
