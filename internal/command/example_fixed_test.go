package command_test

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/command"
)

// fixedType is the type our execute function
// expects to always receive when it's called.
// The type will already be populated with default
// values or values parsed from the arguments passed
// to [command.Execute].
type fixedType struct {
	someField int
}

// BindFlags initializes default values for our type
// and gives the [flag] package the ability to overwrite
// them when parsing flags.
func (ft *fixedType) BindFlags(flagSet *flag.FlagSet) {
	const (
		flagName    = "flag"
		flagUsage   = "an example flag"
		flagDefault = 1
	)
	flagSet.IntVar(&ft.someField, flagName, flagDefault, flagUsage)
}

// MakeFixedCommand can be used to construct
// commands that expect a specific fixed type,
// and optionally, variadic arguments.
func ExampleMakeFixedCommand() {
	var (
		cmd     = newFixedCommand()
		cmdArgs = newFixedArgsCommand()
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

func newFixedCommand() command.Command {
	const (
		name     = "fixed"
		synopsis = "Prints a value."
		usage    = "Call the command with or" +
			" without flags"
	)
	return command.MakeFixedCommand[*fixedType](
		name, synopsis, usage,
		fixedExecute,
	)
}

func fixedExecute(ctx context.Context, settings *fixedType) error {
	fmt.Printf("settings.someField: %d\n", settings.someField)
	return nil
}

func newFixedArgsCommand() command.Command {
	const (
		name     = "fixed-args"
		synopsis = "Prints a value and arguments."
		usage    = "Call the command with or" +
			" without flags or arguments"
	)
	return command.MakeFixedCommand[*fixedType](
		name, synopsis, usage,
		fixedExecuteArgs,
	)
}

func fixedExecuteArgs(ctx context.Context, settings *fixedType, arguments ...string) error {
	if err := fixedExecute(ctx, settings); err != nil {
		return nil
	}
	if len(arguments) > 0 {
		fmt.Printf("arguments: %v\n", arguments)
	}
	return nil
}
