package command

import "os"

type (
	// Option is a functional option.
	// One can be returned by the various constructors
	// before being passed to [MakeCommand].
	Option func(*commandSettings) error

	commandSettings struct {
		usageOutput StringWriter
		subcommands []Command
	}
)

// WithSubcommands provides a command with subcommands.
// Subcommands will be called if the supercommand receives
// arguments that match the subcommand name.
func WithSubcommands(subcommands ...Command) Option {
	return func(settings *commandSettings) error {
		settings.subcommands = subcommands
		return nil
	}
}

// WithUsageOutput sets the writer that is written
// to when [Command.Execute] receives a request for
// help, or returns [ErrUsage].
func WithUsageOutput(output StringWriter) Option {
	return func(settings *commandSettings) error {
		settings.usageOutput = output
		return nil
	}
}

func parseOptions(options ...Option) (*commandSettings, error) {
	set := commandSettings{
		usageOutput: os.Stderr,
	}
	for _, setFunc := range options {
		if err := setFunc(&set); err != nil {
			return nil, err
		}
	}
	return &set, nil
}
