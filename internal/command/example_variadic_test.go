package command_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/djdv/go-filesystem-utils/internal/command"
)

type (
	// settings is the type our execute function
	// will construct on its own.
	settings struct {
		someField int
	}
	// Execute expects to receive a list of `option`s
	// that it will apply to the [settings] struct.
	option func(*settings) error
	// options holds individual [option] values
	// and also satisfies the [command.ExecuteType] constraint.
	options []option
)

const variadicFlagDefault = 1

// BindFlags gives the [flag] package the ability to
// append options to our list during flag parsing.
func (ol *options) BindFlags(flagSet *flag.FlagSet) {
	const (
		flagName  = "flag"
		flagUsage = "an example flag"
	)
	flagSet.Func(flagName, flagUsage, func(parameter string) error {
		parsedValue, err := strconv.Atoi(parameter)
		if err != nil {
			return err
		}
		*ol = append(*ol, func(settings *settings) error {
			settings.someField = parsedValue
			return nil
		})
		return nil
	})
}

// MakeVariadicCommand can be used to construct
// commands that expect a variable amount of parameters.
func ExampleMakeVariadicCommand() {
	var (
		cmd     = newVariadicCommand()
		cmdArgs = newVariadicArgsCommand()
		ctx     = context.TODO()
	)
	if err := cmd.Execute(ctx); err != nil {
		fmt.Fprint(os.Stderr, err)
		return
	}
	if err := cmd.Execute(ctx, "-flag=2"); err != nil {
		fmt.Fprint(os.Stderr, err)
		return
	}
	if err := cmdArgs.Execute(ctx, "-flag=3"); err != nil {
		fmt.Fprint(os.Stderr, err)
		return
	}
	if err := cmdArgs.Execute(ctx, "-flag=4", "a", "b", "c"); err != nil {
		fmt.Fprint(os.Stderr, err)
		return
	}
	// Output:
	// settings.someField: 1
	// settings.someField: 2
	// settings.someField: 3
	// settings.someField: 4
	// arguments: [a b c]
}

func newVariadicCommand() command.Command {
	const (
		name     = "variadic"
		synopsis = "Prints a value."
		usage    = "Call the command with or" +
			" without flags"
	)
	cmd, err := command.MakeVariadicCommand[options](
		name, synopsis, usage,
		variadicExecute,
	)
	if err != nil {
		panic(err)
	}
	return cmd
}

func variadicExecute(ctx context.Context, options ...option) error {
	settings := settings{
		someField: variadicFlagDefault,
	}
	for _, apply := range options {
		if err := apply(&settings); err != nil {
			return err
		}
	}
	fmt.Printf("settings.someField: %d\n", settings.someField)
	return nil
}

func newVariadicArgsCommand() command.Command {
	const (
		name     = "variadic-args"
		synopsis = "Prints a value and arguments."
		usage    = "Call the command with or" +
			" without flags or arguments"
	)
	cmd, err := command.MakeVariadicCommand[options](
		name, synopsis, usage,
		variadicExecuteArgs,
	)
	if err != nil {
		panic(err)
	}
	return cmd
}

func variadicExecuteArgs(ctx context.Context, arguments []string, options ...option) error {
	if err := variadicExecute(ctx, options...); err != nil {
		return nil
	}
	if len(arguments) > 0 {
		fmt.Printf("arguments: %v\n", arguments)
	}
	return nil
}
