package command

import (
	"context"
	"errors"
	"flag"
	"os"
	"strings"
)

// TODO: name and docs
type (
	// Settings is a constraint that permits any reference type
	// which also implements a [FlagBinder] and the help flag hook method.
	Settings[T any] interface {
		*T
		HelpFlag
		FlagBinder
	}

	// TODO: docs
	ExecuteFunc[settings Settings[T], T any] interface {
		func(context.Context, settings) error
	}

	// TODO: docs
	// The primary expected signature of a command's Execute function/method.
	ExecuteFuncArgs[settings Settings[T], T any] interface {
		func(context.Context, settings, ...string) error
	}

	CommandFunc[settings Settings[T], T any] interface {
		ExecuteFunc[settings, T] | ExecuteFuncArgs[settings, T]
	}

	// TODO: docs
	// interface level signature of [CommandFunc].
	commandFunc func(context.Context, ...string) error
	usageFunc   func(StringWriter, *flag.FlagSet) error

	command struct {
		name, synopsis string
		usage          usageFunc
		execute        commandFunc
		subcommands    []Command
	}
)

func MakeCommand[settings Settings[T],
	T any,
	execFunc CommandFunc[settings, T],
](
	name, synopsis, usage string,
	exec execFunc, options ...Option,
) Command {
	constructorSettings, err := parseOptions(options...)
	if err != nil {
		panic(err)
	}
	cmd := &command{
		name:        name,
		synopsis:    synopsis,
		subcommands: constructorSettings.subcommands,
	}
	cmd.usage = wrapUsage[settings](cmd, usage)
	cmd.execute = wrapExecute[settings](constructorSettings.usageOutput, cmd, exec)
	return cmd
}

func (cmd *command) Name() string { return cmd.name }
func (cmd *command) Usage() string {
	output := new(strings.Builder)
	if err := cmd.usage(output, nil); err != nil {
		panic(err)
	}
	return output.String()
}
func (cmd *command) Synopsis() string       { return cmd.synopsis }
func (cmd *command) Subcommands() []Command { return cmd.subcommands }
func (cmd *command) Execute(ctx context.Context, args ...string) error {
	return cmd.execute(ctx, args...)
}

func wrapUsage[settings Settings[T], T any](cmd *command,
	usage string,
) func(StringWriter, *flag.FlagSet) error {
	var (
		name        = cmd.name
		subcommands = cmd.subcommands
	)
	return func(output StringWriter, flagSet *flag.FlagSet) error {
		if output == nil {
			output = os.Stderr
		}
		if flagSet == nil {
			flagSet = flag.NewFlagSet(name, flag.ContinueOnError)
			(settings)(new(T)).BindFlags(flagSet)
		}
		return printHelpText(output, name, usage, flagSet, subcommands...)
	}
}

func wrapExecute[settings Settings[T], T any,
	execFunc CommandFunc[settings, T],
](usageOutput StringWriter, cmd *command, execFn execFunc,
) func(context.Context, ...string) error {
	return func(ctx context.Context, args ...string) error {
		var (
			flagSet, set, err = parseArgs[settings](cmd, args...)
			maybePrintUsage   = func(err error) error {
				if errors.Is(err, ErrUsage) {
					if printErr := cmd.usage(usageOutput, flagSet); printErr != nil {
						return printErr
					}
				}
				return err
			}
		)
		if err != nil {
			return maybePrintUsage(err)
		}
		var (
			subcommands = cmd.subcommands
			haveSubs    = len(subcommands) > 0
			arguments   = flagSet.Args()
			haveArgs    = len(arguments) > 0
		)
		if haveSubs && haveArgs {
			if ran, err := execSub(ctx, subcommands, arguments); ran {
				return err
			}
		}
		var execErr error
		switch execFn := any(execFn).(type) {
		case func(context.Context, settings) error:
			if haveArgs {
				execErr = ErrUsage
			} else {
				execErr = execFn(ctx, set)
			}
		case func(context.Context, settings, ...string) error:
			execErr = execFn(ctx, set, arguments...)
		}
		return maybePrintUsage(execErr)
	}
}

func parseArgs[settings Settings[T], T any](cmd *command, args ...string,
) (*flag.FlagSet, settings, error) {
	flagSet, set, err := parseFlags[settings](cmd.name, args...)
	if err != nil {
		return nil, nil, err
	}
	if set.HelpRequested() {
		return nil, nil, ErrUsage
	}
	return flagSet, set, nil
}

func parseFlags[settings Settings[T], T any](name string, args ...string,
) (*flag.FlagSet, settings, error) {
	var (
		flagSet          = flag.NewFlagSet(name, flag.ContinueOnError)
		set     settings = new(T)
	)
	set.BindFlags(flagSet)
	if err := flagSet.Parse(args); err != nil {
		return nil, nil, err
	}
	return flagSet, set, nil
}

func execSub(ctx context.Context, subcommands []Command, arguments []string) (bool, error) {
	subname := arguments[0]
	for _, subcmd := range subcommands {
		if subcmd.Name() == subname {
			return true, subcmd.Execute(ctx, arguments[1:]...)
		}
	}
	return false, nil
}
