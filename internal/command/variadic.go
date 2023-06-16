package command

import (
	"context"
	"flag"
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
) Command {
	cmd := variadicCommand[TS, T, ET, EC]{
		commandCommon: commandCommon{
			name:     name,
			synopsis: synopsis,
			usage:    usage,
		},
		executeFn: executeFn,
	}
	applyOptions(&cmd.commandCommon, options...)
	return &cmd
}

func (vc *variadicCommand[TS, T, ET, EC]) acceptsArgs() bool {
	_, haveArgs := any(vc.executeFn).(func(context.Context, []string, ...T) error)
	return haveArgs
}

func (vc *variadicCommand[TS, T, ET, EC]) Execute(ctx context.Context, args ...string) error {
	if subcommand, subargs := getSubcommand(vc, args); subcommand != nil {
		return subcommand.Execute(ctx, subargs...)
	}
	var (
		flagSet = newFlagSet(vc.name)
		options TS
	)
	ET(&options).BindFlags(flagSet)
	needHelp, err := vc.parseFlags(flagSet, args...)
	if err != nil {
		return err
	}
	if needHelp {
		err = flag.ErrHelp
	} else {
		err = vc.execute(ctx, flagSet, options...)
	}
	if err != nil {
		acceptsArgs := vc.acceptsArgs()
		return vc.maybePrintUsage(err, acceptsArgs, flagSet)
	}
	return nil
}

func (vc *variadicCommand[TS, T, ET, EC]) execute(ctx context.Context, flagSet *flag.FlagSet, options ...T) error {
	var (
		arguments = flagSet.Args()
		haveArgs  = len(arguments) > 0
		execErr   error
	)
	switch execFn := any(vc.executeFn).(type) {
	case func(context.Context, ...T) error:
		if haveArgs {
			execErr = unexpectedArguments(vc.name, arguments)
			break
		}
		execErr = execFn(ctx, options...)
	case func(context.Context, []string, ...T) error:
		execErr = execFn(ctx, arguments, options...)
	}
	return execErr
}
