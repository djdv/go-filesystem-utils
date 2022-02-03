// Package parameters provides abstractions around program parameters.
// Both creation and consumption, to and from sources such as the command-line,
// environment variables, HTTP requests, etc.
//
// Primarily by utilizing a struct convention, reflection, and the Go runtime to infer
// intent automatically.
package parameters

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/fatih/camelcase"
)

type (
	// SourceID represents a value source/store.
	// Such as the command-line, process environment, etc.
	SourceID uint

	// SetFunc is provided a Settings's Arguments to be set.
	// It should attempt to set the ValueReference,
	// by utilizing the Parameter to search some value store.
	// If SetFunc doesn't have a value for the Parameter,
	// it should relay the Argument in its output channel.
	// (So that subsequent SetFuncs may search their own value sources for it.)
	SetFunc func(context.Context, Arguments, ...TypeParser) (unsetArgs Arguments, _ <-chan error)

	// Parameter contains methods
	// to describe its argument and various names for it.
	Parameter interface {
		// The primary names to be used with this parameter.
		Name(SourceID) string
		// Secondary names to be used with this parameter.
		// E.g. short names, deprecated names, alternate names, etc.
		Aliases(SourceID) []string
		// A string that describes what this parameter influences.
		Description() string
	}
	Parameters []Parameter

	parameter struct {
		namespace,
		name,
		description,
		envPrefix string
		aliases []string
	}

	// Settings are expected to be implemented by structs
	// that follow a specific convention.
	//
	// The library will look for the tag:
	//   `parameters:"settings"`
	// within the struct
	// (either directly or recursively via embedded structs)
	//
	// The first field declared in the struct that contains the tag,
	// will be associated with the first parameter returned from
	// the `Parameters` method of this interface, and so on.
	// (I.e. The order of fields in the struct, and the order of parameters returned,
	// must match.)
	Settings interface {
		Parameters() Parameters
	}

	stringMapperFunc func(r rune) rune

	errorCh <-chan error
)

const (
	_ SourceID = iota
	CommandLine
	Environment
)

var (
	errUnexpectedSourceID = errors.New("unexpected source ID")
	errUnexpectedType     = errors.New("unexpected type")
	errUnassignable       = errors.New("cannot assign")
	errNoTag              = errors.New("no fields contained tag")
	errTooFewFields       = errors.New("not enough fields")
	errMultiPointer       = errors.New("multiple layers of indirection (not supported)")
)

func (params Parameters) String() string {
	parameterNames := make([]string, len(params))
	for i, param := range params {
		parameterNames[i] = param.Name(CommandLine)
	}
	return strings.Join(parameterNames, ", ")
}

func filter(components []string, filters ...stringMapperFunc) []string {
	for _, filter := range filters {
		filtered := make([]string, 0, len(components))
		for _, component := range components {
			if component := strings.Map(filter, component); component != "" {
				filtered = append(filtered, component)
			}
		}
		components = filtered
	}
	return components
}

func filterCli(parameterRune rune) rune {
	if unicode.IsSpace(parameterRune) ||
		parameterRune == '=' {
		return -1
	}
	return parameterRune
}

func filterEnv(keyRune rune) rune {
	if unicode.IsSpace(keyRune) ||
		keyRune == '=' {
		return -1
	}
	if keyRune == '.' {
		return '_'
	}
	return keyRune
}

func filterRuntime(refRune rune) rune {
	// NOTE: references from methods usually look like: `pkg.(*Type).Method`.
	if refRune == '(' ||
		refRune == '*' ||
		refRune == ')' {
		return -1
	}
	return refRune
}

func (parameter parameter) Description() string { return parameter.description }
func (parameter parameter) Name(source SourceID) string {
	switch source {
	case CommandLine:
		return cliName(parameter.name)
	case Environment:
		return envName(parameter.envPrefix, parameter.namespace, parameter.name)
	default:
		err := fmt.Errorf("%w: %v", errUnexpectedSourceID, source)
		panic(err)
	}
}

func cliName(name string) string {
	var (
		splitName = camelcase.Split(name)
		cleaned   = filter(splitName, filterCli)
		clName    = strings.ToLower(strings.Join(cleaned, "-"))
	)
	return clName
}

func envName(prefix, namespace, name string) string {
	var (
		components []string
		splitName  = camelcase.Split(name)
	)
	if prefix != "" {
		splitPrefix := strings.Split(prefix, " ")
		components = append(components, splitPrefix...)
	}
	if namespace != "" {
		splitNamespace := strings.Split(namespace, " ")
		components = append(components, splitNamespace...)
	}
	components = append(components, splitName...)
	var (
		cleaned = filter(components, filterEnv)
		envName = strings.ToUpper(strings.Join(cleaned, "_"))
	)
	return envName
}

func funcNames(instructionPointer uintptr) (namespace, name string) {
	var (
		// Documentation refers to this as a
		// "package path-qualified function name".
		// Typically looks like: `pkgName.referenceName`,
		// `pkgName.referenceName.deeperReference-name`, etc.
		ppqfn = runtime.FuncForPC(instructionPointer).Name()
		names = strings.Split(path.Base(ppqfn), ".")
	)
	namesEnd := len(names)
	if namesEnd < 2 {
		panic(fmt.Sprintf(
			"runtime returned non-standard function name"+
				"\n\tgot: `%s`"+
				"\n\twant format: `$pkgName.$funcName`",
			ppqfn,
		))
	}
	filteredNames := filter([]string{
		names[0],
		names[namesEnd-1],
	}, filterRuntime)

	namespace = filteredNames[0]
	name = filteredNames[1]
	return
}

func (parameter parameter) Aliases(source SourceID) []string {
	aliases := make([]string, 0, len(parameter.aliases))
	switch source {
	case CommandLine:
		for _, name := range parameter.aliases {
			aliases = append(aliases, cliName(name))
		}
		return aliases
	case Environment:
		var (
			prefix    = parameter.envPrefix
			namespace = parameter.namespace
		)
		for _, name := range parameter.aliases {
			aliases = append(aliases, envName(prefix, namespace, name))
		}
		return aliases
	default:
		err := fmt.Errorf("%w: %v", errUnexpectedSourceID, source)
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
		param = parameter{description: description}
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

func parseParameterOptions(param *parameter,
	options ...ParameterOption,
) (namespaceProvided, nameProvided, envPrefixProvided bool) {
	for _, opt := range options {
		switch opt.(type) {
		case parameterNamespaceOpt:
			namespaceProvided = true
		case parameterNameOpt:
			nameProvided = true
		case parameterEnvprefixOpt:
			envPrefixProvided = true
		}
		opt.apply(param)
	}
	return
}

type (
	// ParameterOption is the name for the Parameter constructor's options.
	ParameterOption interface{ apply(*parameter) }

	parameterNamespaceOpt string
	parameterNameOpt      string
	parameterNameAliasOpt string
	parameterEnvprefixOpt string
)

// WithNamespace uses the provided namespace for the Parameter
// rather than a generated one.
func WithNamespace(s string) ParameterOption               { return parameterNamespaceOpt(s) }
func (s parameterNamespaceOpt) apply(parameter *parameter) { parameter.namespace = string(s) }

// WithRootNamespace omits a namespace from the parameter,
// and thus omits any use of it (and/or may otherwise incur special handling)
//
// E.g. environment variables will contain 1 less component,
// such as `prefix_name` rather than `prefix_namespace_name`, etc.
func WithRootNamespace() ParameterOption { return parameterNamespaceOpt("") }

// WithName uses the provided name for the Parameter
// rather than a generated one.
func WithName(s string) ParameterOption               { return parameterNameOpt(s) }
func (s parameterNameOpt) apply(parameter *parameter) { parameter.name = string(s) }

// WithAlias provides alternate names that may be used to refer to the Parameter.
// E.g. short-form versions of a command-line name `parameter` with alias `p`.
// (This option may be provided multiple times for multiple aliases.)
func WithAlias(s string) ParameterOption { return parameterNameAliasOpt(s) }

func (s parameterNameAliasOpt) apply(parameter *parameter) {
	parameter.aliases = append(parameter.aliases, string(s))
}

// WithEnvironmentPrefix uses the provided prefix for the environment-variable
// rather than one generated from the process name.
func WithEnvironmentPrefix(s string) ParameterOption       { return parameterEnvprefixOpt(s) }
func (s parameterEnvprefixOpt) apply(parameter *parameter) { parameter.envPrefix = string(s) }
