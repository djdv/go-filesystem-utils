package command

import (
	"context"
	"flag"
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
) Command {
	cmd := fixedCommand[ET, T, EC]{
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

func (fc *fixedCommand[ET, T, EC]) acceptsArgs() bool {
	_, haveArgs := any(fc.executeFn).(func(context.Context, ET, ...string) error)
	return haveArgs
}

func (fc *fixedCommand[ET, T, EC]) Execute(ctx context.Context, args ...string) error {
	if subcommand, subargs := getSubcommand(fc, args); subcommand != nil {
		return subcommand.Execute(ctx, subargs...)
	}
	var (
		flagSet  = newFlagSet(fc.name)
		settings T
	)
	ET(&settings).BindFlags(flagSet)
	needHelp, err := fc.parseFlags(flagSet, args...)
	if err != nil {
		return err
	}
	if needHelp {
		err = flag.ErrHelp
	} else {
		err = fc.execute(ctx, flagSet, &settings)
	}
	if err != nil {
		acceptsArgs := fc.acceptsArgs()
		return fc.maybePrintUsage(err, acceptsArgs, flagSet)
	}
	return nil
}

func (fc *fixedCommand[ET, T, EC]) execute(ctx context.Context, flagSet *flag.FlagSet, settings ET) error {
	var (
		arguments = flagSet.Args()
		haveArgs  = len(arguments) > 0
		execErr   error
	)
	switch execFn := any(fc.executeFn).(type) {
	case func(context.Context, ET) error:
		if haveArgs {
			execErr = unexpectedArguments(fc.name, arguments)
			break
		}
		execErr = execFn(ctx, settings)
	case func(context.Context, ET, ...string) error:
		execErr = execFn(ctx, settings, arguments...)
	}
	return execErr
}
