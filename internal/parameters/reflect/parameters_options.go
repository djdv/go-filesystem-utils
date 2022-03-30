package reflect

type (
	// ParameterOption is the name for the Parameter constructor's options.
	ParameterOption interface{ apply(*Parameter) }

	parameterNamespaceOpt string
	parameterNameOpt      string
	parameterNameAliasOpt string
	parameterEnvprefixOpt string
)

func parseParameterOptions(param *Parameter,
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

// WithNamespace uses the provided namespace for the Parameter
// rather than a generated one.
func WithNamespace(s string) ParameterOption               { return parameterNamespaceOpt(s) }
func (s parameterNamespaceOpt) apply(parameter *Parameter) { parameter.namespace = string(s) }

// WithRootNamespace omits a namespace from the parameter,
// and thus omits any use of it (and/or may otherwise incur special handling)
//
// E.g. environment variables will contain 1 less component,
// such as `prefix_name` rather than `prefix_namespace_name`, etc.
func WithRootNamespace() ParameterOption { return parameterNamespaceOpt("") }

// WithName uses the provided name for the Parameter
// rather than a generated one.
func WithName(s string) ParameterOption               { return parameterNameOpt(s) }
func (s parameterNameOpt) apply(parameter *Parameter) { parameter.name = string(s) }

// WithAlias provides alternate names that may be used to refer to the Parameter.
// E.g. short-form versions of a command-line name `parameter` with alias `p`.
// (This option may be provided multiple times for multiple aliases.)
func WithAlias(s string) ParameterOption { return parameterNameAliasOpt(s) }

func (s parameterNameAliasOpt) apply(parameter *Parameter) {
	parameter.aliases = append(parameter.aliases, string(s))
}

// WithEnvironmentPrefix uses the provided prefix for the environment-variable
// rather than one generated from the process name.
func WithEnvironmentPrefix(s string) ParameterOption       { return parameterEnvprefixOpt(s) }
func (s parameterEnvprefixOpt) apply(parameter *Parameter) { parameter.envPrefix = string(s) }
