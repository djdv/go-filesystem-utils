package command

import (
	"context"
	"errors"
	"flag"
	"strings"
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
) (Command, error) {
	settings, err := parseOptions(options...)
	if err != nil {
		return nil, err
	}
	return &niladicCommand{
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

func (nc *niladicCommand) Usage() string {
	var (
		output  = new(strings.Builder)
		flagSet = flag.NewFlagSet(nc.name, flag.ContinueOnError)
		unused  bool
	)
	bindHelpFlag(&unused, flagSet)
	const acceptsArgs = false
	if err := nc.printUsage(output, acceptsArgs, flagSet); err != nil {
		panic(err)
	}
	return output.String()
}

func (nc *niladicCommand) Execute(ctx context.Context, args ...string) error {
	if subcommand, subargs := getSubcommand(nc, args); subcommand != nil {
		return subcommand.Execute(ctx, subargs...)
	}
	flagSet := flag.NewFlagSet(nc.name, flag.ContinueOnError)
	needHelp, err := nc.parseFlags(flagSet, args...)
	if err != nil {
		return err
	}
	if needHelp {
		output := nc.usageOutput
		const acceptsArgs = false
		if printErr := nc.printUsage(output, acceptsArgs, flagSet); printErr != nil {
			return errors.Join(printErr, ErrUsage)
		}
		return ErrUsage
	}
	execErr := nc.execute(ctx, flagSet)
	if errors.Is(execErr, ErrUsage) {
		output := nc.usageOutput
		const acceptsArgs = false
		if printErr := nc.printUsage(output, acceptsArgs, flagSet); printErr != nil {
			return errors.Join(printErr, execErr)
		}
	}
	return execErr
}

func (nc *niladicCommand) execute(ctx context.Context, flagSet *flag.FlagSet) error {
	var (
		arguments = flagSet.Args()
		haveArgs  = len(arguments) > 0
	)
	if haveArgs {
		return ErrUsage
	}
	return nc.executeFn(ctx)
}
