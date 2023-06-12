package command

import (
	"context"
	"errors"
	"flag"
	"strings"
)

type (
	// ExecuteMonadic permits functions
	// with these signatures.
	ExecuteMonadic[
		ET ExecuteType[T],
		T any,
	] interface {
		ExecuteMonadicFunc[ET, T] |
			ExecuteDyadicFunc[ET, T]
	}

	// ExecuteMonadicFunc is a variant of [ExecuteNiladicFunc]
	// that also accepts an [ExecuteType].
	ExecuteMonadicFunc[
		ET ExecuteType[T],
		T any,
	] interface {
		func(context.Context, ET) error
	}

	// ExecuteDyadicFunc is a variant of [ExecuteMonadicFunc]
	// that also accepts variadic arguments.
	ExecuteDyadicFunc[
		ET ExecuteType[T],
		T any,
	] interface {
		func(context.Context, ET, ...string) error
	}

	fixedCommand[
		ET ExecuteType[T], T any,
		EC ExecuteMonadic[ET, T],
	] struct {
		executeFn EC
		commandCommon
	}
)

// MakeFixedCommand wraps a function which
// accepts either [ExecuteType] or,
// [ExecuteType] and variadic string parameters,
// which are passed to `executeFn` during [command.Execute].
func MakeFixedCommand[
	ET ExecuteType[T],
	EC ExecuteMonadic[ET, T],
	T any,
](
	name, synopsis, usage string,
	executeFn EC, options ...Option,
) (Command, error) {
	settings, err := parseOptions(options...)
	if err != nil {
		return nil, err
	}
	return &fixedCommand[ET, T, EC]{
		commandCommon: commandCommon{
			name:        name,
			synopsis:    synopsis,
			usage:       usage,
			usageOutput: settings.usageOutput,
			subcommands: settings.subcommands,
		},
		executeFn: executeFn,
	}, nil
}

func (cmd *fixedCommand[ET, T, EC]) Usage() string {
	var (
		output      = new(strings.Builder)
		flagSet     = flag.NewFlagSet(cmd.name, flag.ContinueOnError)
		acceptsArgs = cmd.acceptsArgs()
		unused      bool
	)
	bindHelpFlag(&unused, flagSet)
	(ET)(new(T)).BindFlags(flagSet)
	if err := cmd.printUsage(output, acceptsArgs, flagSet); err != nil {
		panic(err)
	}
	return output.String()
}

func (cmd *fixedCommand[ET, T, EC]) acceptsArgs() bool {
	_, haveArgs := any(cmd.executeFn).(func(context.Context, ET, ...string) error)
	return haveArgs
}

func (cmd *fixedCommand[ET, T, EC]) Execute(ctx context.Context, args ...string) error {
	if subcommand, subargs := getSubcommand(cmd, args); subcommand != nil {
		return subcommand.Execute(ctx, subargs...)
	}
	var (
		needHelp bool
		flagSet  = flag.NewFlagSet(cmd.name, flag.ContinueOnError)
		settings T
	)
	ET(&settings).BindFlags(flagSet)
	needHelp, err := cmd.parseFlags(flagSet, args...)
	if err != nil {
		return err
	}
	if needHelp {
		var (
			output      = cmd.usageOutput
			acceptsArgs = cmd.acceptsArgs()
		)
		if printErr := cmd.printUsage(output, acceptsArgs, flagSet); printErr != nil {
			return errors.Join(printErr, ErrUsage)
		}
		return ErrUsage
	}
	execErr := cmd.execute(ctx, flagSet, &settings)
	if execErr == nil {
		return nil
	}
	if errors.Is(execErr, ErrUsage) {
		var (
			output      = cmd.usageOutput
			acceptsArgs = cmd.acceptsArgs()
		)
		if printErr := cmd.printUsage(output, acceptsArgs, flagSet); printErr != nil {
			return errors.Join(printErr, execErr)
		}
	}
	return execErr
}

func (cmd *fixedCommand[ET, T, EC]) execute(ctx context.Context, flagSet *flag.FlagSet, settings ET) error {
	var (
		arguments = flagSet.Args()
		haveArgs  = len(arguments) > 0
		execErr   error
	)
	switch execFn := any(cmd.executeFn).(type) {
	case func(context.Context, ET) error:
		if haveArgs {
			execErr = ErrUsage
			break
		}
		execErr = execFn(ctx, settings)
	case func(context.Context, ET, ...string) error:
		execErr = execFn(ctx, settings, arguments...)
	}
	return execErr
}
