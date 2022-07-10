package command

import "io"

type (
	Option func(*commandSettings) error

	commandSettings struct {
		usageOutput io.StringWriter
		subcmds     []Command
		niladic     bool
	}
)

func WithSubcommands(subcommands ...Command) Option {
	return func(settings *commandSettings) error {
		settings.subcmds = subcommands
		return nil
	}
}

// TODO: docs; this is where the usage text gets printed
// when args are not what execute() expects for the command.
// Note that we're going to resolve nil to stderr
// so make the user aware of this.
func WithUsageOutput(output io.StringWriter) Option {
	return func(settings *commandSettings) error {
		settings.usageOutput = output
		return nil
	}
}

func WithoutArguments(niladic bool) Option {
	return func(settings *commandSettings) error {
		settings.niladic = niladic
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
