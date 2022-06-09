package option

type (
	// ConstructorOption is a functional options interface
	// for `cmds.Option` constructors.
	ConstructorOption   interface{ apply(*constructorSettings) }
	constructorSettings struct {
		userConstructors []Constructor
		withBuiltin      bool
	}

	constructorOption struct{ Constructor }
	builtinOption     bool
)

func parseConstructorOptions(options ...ConstructorOption) (settings constructorSettings) {
	for _, opt := range options {
		opt.apply(&settings)
	}
	return
}

// WithBuiltin sets whether cmdslib native options
// (such as `--help`, `--timeout`, and more) should be constructed.
func WithBuiltin(b bool) ConstructorOption             { return builtinOption(b) }
func (b builtinOption) apply(set *constructorSettings) { set.withBuiltin = bool(b) }

// WithConstructor appends the OptionConstructor to an internal handler list.
func WithConstructor(c Constructor) ConstructorOption { return constructorOption{c} }

func (c constructorOption) apply(set *constructorSettings) {
	set.userConstructors = append(set.userConstructors, c.Constructor)
}
