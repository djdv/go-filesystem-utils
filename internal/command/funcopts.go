package command

type (
	Option func(*commandSettings) error

	commandSettings struct {
		usageOutput StringWriter
		subcommands []Command
	}
)

func WithSubcommands(subcommands ...Command) Option {
	return func(settings *commandSettings) error {
		settings.subcommands = subcommands
		return nil
	}
}

// TODO: docs; this is where the usage text gets printed
// when args are not what execute() expects for the command.
// Note that we're going to resolve nil to stderr
// so make the user aware of this.
func WithUsageOutput(output StringWriter) Option {
	return func(settings *commandSettings) error {
		settings.usageOutput = output
		return nil
	}
}

func parseOptions(options ...Option) (*commandSettings, error) {
	set := new(commandSettings)
	for _, setFunc := range options {
		if err := setFunc(set); err != nil {
			return nil, err
		}
	}
	return set, nil
}
