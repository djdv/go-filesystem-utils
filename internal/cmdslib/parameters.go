package cmdslib

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type (
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

// Either, take in a callback, or return a channel.
// ^ do the latter, use ForEach within params?
// TODO: replace ParameterMaker with this.
//func ReflectParameters[in any](fn func(reflect.StructField) Parameter.Parameters) parameters.Parameters {
//func ReflectParameters[in any](ctx context.Context) StructFields {
//
// TODO: needs to take in at least a `descriptions [$len(fields)]string`
// Either take in things like namespace and return params
// or return chan of partial params for caller to range over and complete.
func ReflectParameters[settings any, setPtr SettingsConstraint[settings]](ctx context.Context,
	partialParams []CmdsParameter) parameters.Parameters {
	/* going to need this:
	// NewParameter constructs a parameter using either the provided options,
	// or a set of defaults (derived from the calling function's name, pkg, and binary name).
	*/

	// descriptions come in
	// templates come out
	// provided by us: name, namespace, envprefix
	// provided by caller: descriptions
	// caller gets back channel, post-modify however they want,
	// or can just raw return (if we change interface return type to chan for Parameters).
	// ^ make and use a shim for now, search and replace later
	// .Params{ return  prototype(descs...) => chan => slice }
	// ^ later .Params { return final(descs...) => chan }

	typ, err := checkType[settings, setPtr]()
	if err != nil {
		panic(err)
	}

	funcLocation, _, _, ok := runtime.Caller(1)
	if !ok {
		panic("runtime could not get program counter address for function")
	}
	namespace, _ := funcNames(funcLocation)
	if namespace == "main" {
		namespace = ""
	}

	procName := filepath.Base(os.Args[0])
	envPrefix := strings.TrimSuffix(
		procName, filepath.Ext(procName),
	)
	// Meta ^^ extract?

	// FIXME:
	// should we just do root params? (generate only, no expand)
	// caller can combine sub-structs manually with CtxJoin.
	// Our loop swaps target? range over params, pull from fields?
	// or just keep the same.
	params := make(chan parameters.Parameter, len(partialParams))
	go func() {
		defer close(params)
		var (
			fieldIndex int
			baseFields = generateFields(ctx, typ)
			allFields  = expandFields(ctx, baseFields)
		)
		log.Println("params:", partialParams)
		for field := range allFields {
			log.Println("field:", field.Name)
			param := partialParams[fieldIndex]
			log.Println("param:", param.Description())
			fieldIndex++
			fieldName := field.Name
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
			case <-ctx.Done():
				return
			}
		}
	}()

	return params
}

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
