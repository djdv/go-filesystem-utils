package reflect

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type (
	SettingsConstraint[settingsPtr any] interface {
		parameters.Settings
		*settingsPtr
	}

	Parameter struct {
		namespace,
		name,
		description,
		envPrefix string
		aliases []string
	}
)

func (parameter Parameter) Description() string { return parameter.description }
func (parameter Parameter) Name(source parameters.SourceID) string {
	switch source {
	case parameters.CommandLine:
		return cliName(parameter.name)
	case parameters.Environment:
		return envName(parameter.envPrefix, parameter.namespace, parameter.name)
	default:
		err := fmt.Errorf("%w: %v", parameters.ErrUnexpectedSourceID, source)
		panic(err)
	}
}

func (parameter Parameter) Aliases(source parameters.SourceID) []string {
	aliases := make([]string, 0, len(parameter.aliases))
	switch source {
	case parameters.CommandLine:
		for _, name := range parameter.aliases {
			aliases = append(aliases, cliName(name))
		}
		return aliases
	case parameters.Environment:
		var (
			prefix    = parameter.envPrefix
			namespace = parameter.namespace
		)
		for _, name := range parameter.aliases {
			aliases = append(aliases, envName(prefix, namespace, name))
		}
		return aliases
	default:
		err := fmt.Errorf("%w: %v", parameters.ErrUnexpectedSourceID, source)
		panic(err)
	}
}

// NewParameter constructs a parameter using either the provided options,
// or a set of defaults (derived from the calling function's name, pkg, and binary name).
func NewParameter(description string, options ...ParameterOption) Parameter {
	var (
		// NOTE: We want to retain comparability, so we don't return a pointer here.
		// Duplicate inputs should produce (Go) equivalent outputs.
		// I.e. usable with equality operator `==`, type `comparable`, etc.
		param = Parameter{description: description}
		namespaceProvided,
		nameProvided,
		envPrefixProvided = parseParameterOptions(&param, options...)
	)

	// If information wasn't provided by the caller,
	// fill in defaults.
	if !namespaceProvided || !nameProvided {
		funcLocation, _, _, ok := runtime.Caller(1)
		if !ok {
			panic("runtime could not get program counter address for function")
		}
		namespace, name := funcNames(funcLocation)
		if !namespaceProvided {
			param.namespace = namespace
		}
		if !nameProvided {
			param.name = name
		}
	}
	if !envPrefixProvided {
		procName := filepath.Base(os.Args[0])
		param.envPrefix = strings.TrimSuffix(
			procName, filepath.Ext(procName),
		)
	}

	return param
}
