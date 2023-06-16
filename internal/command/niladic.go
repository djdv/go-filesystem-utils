package command

import (
	"context"
	"flag"
)

type (
	// ExecuteNiladicFunc may be used
	// as a command's Execute function.
	ExecuteNiladicFunc func(context.Context) error
	niladicCommand     struct {
		executeFn ExecuteNiladicFunc
		commandCommon
	}
)

// MakeNiladicCommand returns a command
// that wraps `executeFn`.
func MakeNiladicCommand(
	name, synopsis, usage string,
	executeFn ExecuteNiladicFunc,
	options ...Option,
) Command {
	cmd := niladicCommand{
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

func (nc *niladicCommand) Execute(ctx context.Context, args ...string) error {
	if subcommand, subargs := getSubcommand(nc, args); subcommand != nil {
		return subcommand.Execute(ctx, subargs...)
	}
	var (
		flagSet       = newFlagSet(nc.name)
		needHelp, err = nc.parseFlags(flagSet, args...)
	)
	if err != nil {
		return err
	}
	if needHelp {
		err = flag.ErrHelp
	} else {
		err = nc.execute(ctx, flagSet)
	}
	if err != nil {
		const acceptsArgs = false
		return nc.maybePrintUsage(err, acceptsArgs, flagSet)
	}
	return nil
}

func (nc *niladicCommand) execute(ctx context.Context, flagSet *flag.FlagSet) error {
	var (
		arguments = flagSet.Args()
		haveArgs  = len(arguments) > 0
	)
	if haveArgs {
		return unexpectedArguments(nc.name, arguments)
	}
	return nc.executeFn(ctx)
}
