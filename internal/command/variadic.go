package command

import (
	"context"
	"errors"
	"flag"
	"strings"
)

type (
	// ExecuteVariadic permits functions
	// with these signatures.
	ExecuteVariadic[
		ET ExecuteType[ST],
		ST ~[]VT,
		VT any,
	] interface {
		ExecuteVariadicFunc[ET, ST, VT] |
			ExecuteArgumentsVariadicFunc[ET, ST, VT]
	}

	// ExecuteVariadicFunc is a variant of [ExecuteNiladicFunc]
	// that also accepts a variadic [ExecuteType].
	ExecuteVariadicFunc[
		ET ExecuteType[ST],
		ST ~[]VT,
		VT any,
	] interface {
		func(context.Context, ...VT) error
	}

	// ExecuteArgumentsVariadicFunc is a variant of [ExecuteVariadicFunc]
	// that also accepts arguments.
	ExecuteArgumentsVariadicFunc[
		ET ExecuteType[ST],
		ST ~[]VT,
		VT any,
	] interface {
		func(context.Context, []string, ...VT) error
	}

	variadicCommand[
		TS ~[]VT,
		VT any,
		ET ExecuteType[TS],
		EC ExecuteVariadic[ET, TS, VT],
	] struct {
		executeFn EC
		commandCommon
	}
)

// MakeVariadicCommand wraps a function which
// accepts either variaic [ExecuteType] or,
// a slice of string parameters and variadic [ExecuteType]
// which are passed to `executeFn` during [command.Execute].
func MakeVariadicCommand[
	TS ~[]T,
	ET ExecuteType[TS],
	EC ExecuteVariadic[ET, TS, T],
	T any,
](
	name, synopsis, usage string,
	executeFn EC, options ...Option,
) (Command, error) {
	settings, err := parseOptions(options...)
	if err != nil {
		return nil, err
	}
	return &variadicCommand[TS, T, ET, EC]{
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

func (cmd *variadicCommand[TS, T, ET, EC]) Usage() string {
	var (
		output      = new(strings.Builder)
		flagSet     = flag.NewFlagSet(cmd.name, flag.ContinueOnError)
		acceptsArgs = cmd.acceptsArgs()
		unused      bool
	)
	bindHelpFlag(&unused, flagSet)
	(ET)(new(TS)).BindFlags(flagSet)
	if err := cmd.printUsage(output, acceptsArgs, flagSet); err != nil {
		panic(err)
	}
	return output.String()
}

func (cmd *variadicCommand[TS, T, ET, EC]) acceptsArgs() bool {
	_, haveArgs := any(cmd.executeFn).(func(context.Context, []string, ...T) error)
	return haveArgs
}

func (cmd *variadicCommand[TS, T, ET, EC]) Execute(ctx context.Context, args ...string) error {
	if subcommand, subargs := getSubcommand(cmd, args); subcommand != nil {
		return subcommand.Execute(ctx, subargs...)
	}
	var (
		needHelp bool
		flagSet  = flag.NewFlagSet(cmd.name, flag.ContinueOnError)
		options  TS
	)
	ET(&options).BindFlags(flagSet)
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
	execErr := cmd.execute(ctx, flagSet, options...)
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

func (cmd *variadicCommand[TS, T, ET, EC]) execute(ctx context.Context, flagSet *flag.FlagSet, options ...T) error {
	var (
		arguments = flagSet.Args()
		haveArgs  = len(arguments) > 0
		execErr   error
	)
	switch execFn := any(cmd.executeFn).(type) {
	case func(context.Context, ...T) error:
		if haveArgs {
			execErr = ErrUsage
			break
		}
		execErr = execFn(ctx, options...)
	case func(context.Context, []string, ...T) error:
		execErr = execFn(ctx, arguments, options...)
	}
	return execErr
}
