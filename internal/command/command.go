package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/djdv/go-filesystem-utils/internal/generic"
)

type (
	// Command is a decorated function ready to be executed.
	Command interface {
		// Name returns a human friendly name of the command,
		// which may be used to identify commands
		// as well as decorate user facing help-text.
		Name() string

		// Synopsis returns a single-line short string describing the command.
		Synopsis() string

		// Usage returns an arbitrarily long string explaining how to use the command.
		Usage() string

		// Subcommands returns a list of subcommands (if any).
		Subcommands() []Command

		// Execute executes the command, with or without any arguments.
		Execute(ctx context.Context, args ...string) error
	}
	// ExecuteType is a constraint that permits any reference
	// type that can bind its value(s) to flags.
	ExecuteType[T any] interface {
		*T
		FlagBinder
	}
	// A FlagBinder should call relevant [flag.FlagSet] methods
	// to bind each of it's variable references with the FlagSet.
	// E.g. a struct would pass references of its fields
	// to `FlagSet.Var(&structABC.fieldXYZ, ...)`.
	FlagBinder interface {
		BindFlags(*flag.FlagSet)
	}
	// Option is a functional option.
	// One can be returned by the various constructors
	// before being passed to [MakeCommand].
	Option        func(*commandCommon)
	commandCommon struct {
		name, synopsis, usage string
		usageOutput           io.Writer
		subcommands           []Command
	}
)

// ErrUsage may be returned from [command.Execute] if the provided arguments
// do not match the expectations of the given command.
// E.g. too few/many arguments, invalid value/formats, etc.
const ErrUsage = generic.ConstError("command called with unexpected arguments")

// WithSubcommands provides a command with subcommands.
// Subcommands will be called if the supercommand receives
// arguments that match the subcommand name.
func WithSubcommands(subcommands ...Command) Option {
	return func(settings *commandCommon) {
		settings.subcommands = subcommands
	}
}

// WithUsageOutput sets the writer that is written
// to when [Command.Execute] receives a request for
// help, or returns [ErrUsage].
func WithUsageOutput(output io.Writer) Option {
	return func(settings *commandCommon) {
		settings.usageOutput = output
	}
}

// SubcommandGroup returns a command that only defers to subcommands.
// Trying to execute the command itself will return [ErrUsage].
func SubcommandGroup(name, synopsis string, subcommands []Command, options ...Option) Command {
	const usage = "Must be called with a subcommand."
	return MakeNiladicCommand(name, synopsis, usage,
		func(context.Context) error {
			// This command only holds subcommands
			// and has no functionality on its own.
			return ErrUsage
		},
		append(options, WithSubcommands(subcommands...))...,
	)
}

func (cmd *commandCommon) Name() string           { return cmd.name }
func (cmd *commandCommon) Synopsis() string       { return cmd.synopsis }
func (cmd *commandCommon) Subcommands() []Command { return generic.CloneSlice(cmd.subcommands) }
func (cmd *commandCommon) parseFlags(flagSet *flag.FlagSet, arguments ...string) (bool, error) {
	var needHelp bool
	bindHelpFlag(&needHelp, flagSet)
	// Package [flag] has implicit handling for `-help` and `-h` flags.
	// If they're not explicitly defined, but provided as arguments,
	// [flag] will call `Usage` before returning from `Parse`.
	// We want to temporarily disable any built-in printing, to assure
	// our printers are used exclusively. (For both help text and errors)
	var (
		originalUsage  = flagSet.Usage
		originalOutput = flagSet.Output()
	)
	flagSet.Usage = func() { /* NOOP */ }
	flagSet.SetOutput(io.Discard)
	err := flagSet.Parse(arguments)
	flagSet.SetOutput(originalOutput)
	flagSet.Usage = originalUsage
	if err == nil {
		return needHelp, nil
	}
	if errors.Is(err, flag.ErrHelp) {
		needHelp = true
		return needHelp, nil
	}
	return needHelp, err
}

func bindHelpFlag(value *bool, flagSet *flag.FlagSet) {
	const (
		helpName  = "help"
		helpUsage = "prints out this help text"
	)
	flagSet.BoolVar(value, helpName, false, helpUsage)
}

func getSubcommand(command Command, arguments []string) (Command, []string) {
	if len(arguments) == 0 {
		return nil, nil
	}
	subname := arguments[0]
	for _, subcommand := range command.Subcommands() {
		if subcommand.Name() != subname {
			continue
		}
		subarguments := arguments[1:]
		if hypoCmd, hypoArgs := getSubcommand(subcommand, subarguments); hypoCmd != nil {
			return hypoCmd, hypoArgs
		}
		return subcommand, subarguments
	}
	return nil, nil
}

func (cmd *commandCommon) printUsage(output io.Writer, acceptsArgs bool, flagSet *flag.FlagSet) error {
	if output == nil {
		return nil
	}
	var (
		wErr  error
		name  = cmd.name
		write = func(s string) {
			if wErr != nil {
				return
			}
			_, wErr = io.WriteString(output, s)
		}
		subcommands = cmd.subcommands
		haveSubs    = len(subcommands) > 0
		haveFlags   bool
	)
	flagSet.VisitAll(func(*flag.Flag) { haveFlags = true })
	write(cmd.usage + "\n\n")

	write("Usage:\n\t" + name)
	if haveSubs {
		write(" subcommand")
	}
	if haveFlags {
		write(" [flags]")
	}
	if acceptsArgs {
		write(" ...arguments")
	}
	write("\n\n")

	write("Flags:\n")
	flagSetOutput := flagSet.Output()
	flagSet.SetOutput(output)
	flagSet.PrintDefaults()
	flagSet.SetOutput(flagSetOutput)
	write("\n")
	if haveSubs {
		write("Subcommands:\n")
		if err := printCommandsTable(output, subcommands); err != nil {
			return err
		}
	}
	return wErr
}

func printCommandsTable(output io.Writer, subcommands []Command) error {
	tabWriter := tabwriter.NewWriter(output, 0, 0, 0, ' ', 0)
	for _, subcommand := range subcommands {
		if _, pErr := fmt.Fprintf(tabWriter,
			"  %s\t - %s\n", // 2 leading spaces to match [flag] behaviour.
			subcommand.Name(), subcommand.Synopsis(),
		); pErr != nil {
			return pErr
		}
	}
	if _, err := fmt.Fprintln(tabWriter); err != nil {
		return err
	}
	return tabWriter.Flush()
}

func applyOptions(settings *commandCommon, options ...Option) {
	for _, apply := range options {
		apply(settings)
	}
}
