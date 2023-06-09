package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
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

	// Settings is a constraint that permits
	// any reference type
	// that can bind its value(s) to flags
	// and distinguish requests for help.
	Settings[T any] interface {
		*T
		FlagBinder
		HelpRequested() bool
	}

	// A FlagBinder should call the relevant `Var` methods of the [flag.FlagSet],
	// with each of it's flag variable references.
	// E.g. a struct would pass pointers to each of its fields,
	// to `FlagSet.Var(&structABC.fieldXYZ, ...)`.
	FlagBinder interface {
		BindFlags(*flag.FlagSet)
	}

	// ExecuteFunc may be used as a command's Execute function
	// if it only accepts flags, not arguments.
	ExecuteFunc[settings Settings[T], T any] interface {
		func(context.Context, settings) error
	}

	// ExecuteFuncArgs may be used as a command's Execute function
	// if it expects to receive arguments in addition to flags.
	ExecuteFuncArgs[settings Settings[T], T any] interface {
		func(context.Context, settings, ...string) error
	}

	// ExecuteConstraint is satisfied by any of the execute function signatures.
	ExecuteConstraint[settings Settings[T], T any] interface {
		ExecuteFunc[settings, T] | ExecuteFuncArgs[settings, T]
	}

	// StringWriter is a composite interface,
	// used when printing user facing text.
	// (We require [io.Writer] to interface with the Go
	// standard library's [flag] package, but otherwise use
	// [io.StringWriter] internally.)
	StringWriter interface {
		io.Writer
		io.StringWriter
	}

	command[
		EF ExecuteConstraint[S, T],
		S Settings[T],
		T any,
	] struct {
		name, synopsis, usage string
		usageOutput           StringWriter
		execute               EF
		subcommands           []Command
	}
)

// ErrUsage may be returned from Execute if the provided arguments
// do not match the expectations of the given command.
// E.g. arguments in the wrong format/type, too few/many arguments, etc.
const ErrUsage = generic.ConstError("command called with unexpected arguments")

// MustMakeCommand wraps MakeCommand,
// and will panic if it encounters an error.
func MustMakeCommand[
	settings Settings[T], T any,
	execFunc ExecuteConstraint[settings, T],
](
	name, synopsis, usage string,
	execFn execFunc, options ...Option,
) Command {
	cmd, err := MakeCommand[settings](name, synopsis, usage, execFn, options...)
	if err != nil {
		panic(err)
	}
	return cmd
}

// MakeCommand returns a command that will
// receive a parsed [Settings] when executed.
func MakeCommand[
	settings Settings[T], T any,
	execFunc ExecuteConstraint[settings, T],
](
	name, synopsis, usage string,
	execFn execFunc, options ...Option,
) (Command, error) {
	constructorSettings, err := parseOptions(options...)
	if err != nil {
		return nil, err
	}
	return &command[execFunc, settings, T]{
		name:        name,
		synopsis:    synopsis,
		usage:       usage,
		usageOutput: constructorSettings.usageOutput,
		subcommands: constructorSettings.subcommands,
		execute:     execFn,
	}, nil
}

func (cmd *command[EF, S, T]) Name() string { return cmd.name }
func (cmd *command[EF, S, T]) Usage() string {
	output := new(strings.Builder)
	if err := cmd.printUsage(output, nil); err != nil {
		panic(err)
	}
	return output.String()
}
func (cmd *command[EF, S, T]) Synopsis() string       { return cmd.synopsis }
func (cmd *command[EF, S, T]) Subcommands() []Command { return cmd.subcommands }

// Execute
//   - parses arguments
//   - checks [command.HelpFlag]
//   - checks argc against func arity.
//   - may call [command.execute]
//   - may print [command.usage]
func (cmd *command[EF, S, T]) Execute(ctx context.Context, args ...string) error {
	if subcommand, subargs := getSubcommand(cmd, args); subcommand != nil {
		return subcommand.Execute(ctx, subargs...)
	}
	var (
		needHelp bool
		flagSet    = flag.NewFlagSet(cmd.name, flag.ContinueOnError)
		settings S = new(T)
	)
	settings.BindFlags(flagSet)
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
	if err := flagSet.Parse(args); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			return err
		}
		needHelp = true
	}
	if needHelp || settings.HelpRequested() {
		flagSet.SetOutput(originalOutput)
		flagSet.Usage = originalUsage
		if printErr := cmd.printUsage(cmd.usageOutput, flagSet); printErr != nil {
			return errors.Join(printErr, ErrUsage)
		}
		return ErrUsage
	}
	var (
		arguments = flagSet.Args()
		haveArgs  = len(arguments) > 0
		execErr   error
	)
	switch execFn := any(cmd.execute).(type) {
	case func(context.Context, S) error:
		if haveArgs {
			execErr = ErrUsage
			break
		}
		execErr = execFn(ctx, settings)
	case func(context.Context, S, ...string) error:
		execErr = execFn(ctx, settings, arguments...)
	}
	if execErr == nil {
		return nil
	}
	if errors.Is(execErr, ErrUsage) {
		if printErr := cmd.printUsage(cmd.usageOutput, flagSet); printErr != nil {
			return errors.Join(printErr, execErr)
		}
	}
	return execErr
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

func (cmd *command[EF, S, T]) printUsage(output StringWriter, flagSet *flag.FlagSet) error {
	if output == nil {
		output = os.Stderr
	}
	var (
		err   error
		name  = cmd.name
		write = func(s string) {
			if err != nil {
				return
			}
			_, err = output.WriteString(s)
		}
		subcommands    = cmd.subcommands
		haveSubs       = len(subcommands) > 0
		haveFlags      bool
		_, acceptsArgs = any(cmd.execute).(func(context.Context, S, ...string) error)
	)
	if flagSet == nil {
		flagSet = flag.NewFlagSet(name, flag.ContinueOnError)
		(S)(new(T)).BindFlags(flagSet)
	}
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
		var (
			tabWriter = tabwriter.NewWriter(output, 0, 0, 0, ' ', 0)
			subTail   = len(subcommands) - 1
		)
		for i, subcommand := range subcommands {
			if _, pErr := fmt.Fprintf(
				tabWriter, "  %s\t - %s\n",
				subcommand.Name(), subcommand.Synopsis(),
			); pErr != nil {
				return pErr
			}
			if i == subTail {
				fmt.Fprintln(tabWriter)
			}
		}
		if fErr := tabWriter.Flush(); fErr != nil {
			return fErr
		}
	}
	return err
}
