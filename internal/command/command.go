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

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
)

type (
	// Settings is a constraint that permits any reference type
	// which also implements [FlagBinder] and [HelpFlag].
	Settings[T any] interface {
		*T
		HelpFlag
		FlagBinder
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

// MakeCommand returns a command that will
// receive a parsed [Settings] when executed.
func MakeCommand[
	settings Settings[T], T any,
	execFunc ExecuteConstraint[settings, T],
](
	name, synopsis, usage string,
	execFn execFunc, options ...Option,
) Command {
	constructorSettings, err := parseOptions(options...)
	if err != nil {
		panic(err)
	}
	return &command[execFunc, settings, T]{
		name:        name,
		synopsis:    synopsis,
		usage:       usage,
		usageOutput: constructorSettings.usageOutput,
		subcommands: constructorSettings.subcommands,
		execute:     execFn,
	}
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
		flagSet    = flag.NewFlagSet(cmd.name, flag.ContinueOnError)
		settings S = new(T)
	)
	settings.BindFlags(flagSet)
	if err := flagSet.Parse(args); err != nil {
		return err
	}
	if settings.Help() {
		if printErr := cmd.printUsage(cmd.usageOutput, flagSet); printErr != nil {
			return fserrors.Join(printErr, ErrUsage)
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
			execErr = fserrors.Join(printErr, execErr)
		}
	}
	return execErr
}

func getSubcommand(command Command, arguments []string) (Command, []string) {
	subcommands := command.Subcommands()
	if len(subcommands) == 0 ||
		len(arguments) == 0 {
		return nil, arguments
	}
	subname := arguments[0]
	for _, subcommand := range subcommands {
		if subcommand.Name() == subname {
			arguments = arguments[1:]
			if s, a := getSubcommand(subcommand, arguments); s != nil {
				return s, a
			}
			return subcommand, arguments
		}
	}
	return nil, arguments
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
