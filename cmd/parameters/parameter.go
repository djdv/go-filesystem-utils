package parameters

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fatih/camelcase"
)

type (
	// NOTE: API changed, this would be easier to implement now.
	// TODO: go generate the documentation for these types automatically
	// instances => template => source
	// {{fn}} produces {{Parameter}} with instance for {{Argument}}
	// CLI: --{{name}} Env:{{$EnvPrefix}}{{toUpper name}}
	// TODO: docs
	// Parameter defines a method to describe its argument
	// and methods to retrieve references to it.
	// (such as command line options, process environment variables, etc.),
	Parameter interface {
		// Describes the argument value of this parameter,
		// and its influences when set.
		Description() string
		// Key to use for command-line interfaces.
		CommandLine() string
		Environment() string
	}
	Parameters []Parameter

	// TODO: document type magic
	Settings interface {
		Parameters() Parameters
	}

	parameter struct {
		namespace,
		name,
		description,
		envPrefix string
	}
)

func (params Parameters) String() string {
	parameterNames := make([]string, len(params))
	for i, param := range params {
		parameterNames[i] = param.CommandLine()
	}
	return strings.Join(parameterNames, ", ")
}

func (parameter parameter) Description() string { return parameter.description }
func (parameter parameter) CommandLine() string {
	return strings.ToLower(
		strings.Join(
			camelcase.Split(
				parameter.name), "-"))
}

func (parameter parameter) Environment() string {
	var (
		paramComponents = append([]string{
			parameter.envPrefix,
			parameter.namespace,
		},
			camelcase.Split(parameter.name)...,
		)
		envComponents = make([]string, 0, len(paramComponents))
	)
	for _, component := range paramComponents {
		if component != "" {
			// NOTE: Top-level commands are likely to have a blank namespace.
			// We omit any blanks in the ENV_VAR_FORMAT_USED_HERE.
			envComponents = append(envComponents, component)
		}
	}

	return strings.ToUpper(strings.Join(envComponents, "_"))
}

// TODO: document
func NewParameter(description string, options ...ParameterOption) Parameter {
	var (
		// Options that were provided.
		namespace, name, envPrefix bool
		param                      = parameter{description: description}
	)
	for _, opt := range options {
		switch opt.(type) {
		case parameterNamespaceOpt:
			namespace = true
		case parameterNameOpt:
			name = true
		case parameterEnvprefixOpt:
			envPrefix = true
		}
		opt.apply(&param)
	}

	// If information wasn't provided by the caller,
	// fill in defaults.
	if !namespace || !name {
		pc, _, _, ok := runtime.Caller(1)
		if !ok {
			panic("runtime could not get program counter address for function")
		}
		var (
			// "package path-qualified function name"
			ppqfn = runtime.FuncForPC(pc).Name()
			// `pkgName.referenceName`
			pair = strings.Split(path.Base(ppqfn), ".")
		)
		if len(pair) != 2 {
			panic(fmt.Sprintf(
				"runtime returned non-standard function name - expecting format `pkg.funcName` got `%s`",
				ppqfn))
		}
		if !namespace {
			param.namespace = pair[0]
		}
		if !name {
			param.name = pair[1]
		}
	}
	if !envPrefix {
		procName := filepath.Base(os.Args[0])
		param.envPrefix = strings.TrimSuffix(
			procName, filepath.Ext(procName),
		)
	}

	// NOTE: Parameters are returned as values
	// so that their interface representations can be compared.
	// ParamA() == ParamA() == true
	return param
}

type (
	ParameterOption interface{ apply(*parameter) }

	parameterNamespaceOpt string
	parameterNameOpt      string
	parameterEnvprefixOpt string
)

// WithNamespace uses the provided namespace for the Parameter
// rather than a generated one.
func WithNamespace(s string) ParameterOption               { return parameterNamespaceOpt(s) }
func (s parameterNamespaceOpt) apply(parameter *parameter) { parameter.namespace = string(s) }

// WithRootNamespace omits a namespace from the parameter,
// and thus omits any use of it, and/or may otherwise incur special handling.
// E.g. environment variables will contain 1 less component,
// such as `prefix_name` rather than `prefix_namespace_name`, etc.
func WithRootNamespace() ParameterOption { return parameterNamespaceOpt("") }

// WithName uses the provided name for the Parameter
// rather than a generated one.
func WithName(s string) ParameterOption               { return parameterNameOpt(s) }
func (s parameterNameOpt) apply(parameter *parameter) { parameter.name = string(s) }

// WithEnvironmentPrefix uses the provided prefix for the environment-variable
// rather than one generated from the process name.
func WithEnvironmentPrefix(s string) ParameterOption       { return parameterEnvprefixOpt(s) }
func (s parameterEnvprefixOpt) apply(parameter *parameter) { parameter.envPrefix = string(s) }
