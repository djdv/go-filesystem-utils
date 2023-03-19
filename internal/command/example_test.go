package command_test

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/command"
)

const (
	synopisSuffix = " synopis"
	usageSuffix   = " usage"

	parentName     = "fooSenior"
	childName      = "fooJunior"
	grandChildName = "fooIII"

	someFlagName = "some-flag"
)

type exampleSettings struct {
	command.HelpArg
	someField bool
}

func (ts *exampleSettings) BindFlags(fs *flag.FlagSet) {
	ts.HelpArg.BindFlags(fs)
	fs.BoolVar(&ts.someField, someFlagName, false, "some flag help text")
}

// main function expects sub commands only.
// This should be noted in the usage text of a real command.
// (Which will be printed if [command.ErrUsage] or an error
// wrapping it is returned.)
func mainExecute(ctx context.Context, settings *command.HelpArg) error {
	return command.ErrUsage
}

// subExecute expects flags and/or arguments.
func subExecute(ctx context.Context,
	settings *exampleSettings, args ...string,
) error {
	if len(args) > 0 {
		fmt.Println(args)
	}
	if settings.someField {
		fmt.Println("got flag")
	}
	return nil
}

// subSubExecute expects flags but no arguments.
func subSubExecute(ctx context.Context,
	settings *exampleSettings,
) error {
	// Reusing execute functions isn't necessary,
	// but in this case it's possible, so we do it.
	return subExecute(ctx, settings)
}

func ExampleMakeCommand() {
	const (
		cmdName     = parentName
		cmdSynonpis = cmdName + synopisSuffix
		cmdUsage    = cmdName + usageSuffix

		subName     = childName
		subSynonpis = subName + synopisSuffix
		subUsage    = subName + usageSuffix

		subSubName     = childName
		subSubSynonpis = subSubName + synopisSuffix
		subSubUsage    = subSubName + usageSuffix
	)

	var (
		// The output options don't normally need to be specified.
		// And defaults to `os.Stderr`.
		// We only need to output to `os.Stdout` for `go test` purposes.
		output  = os.Stdout
		deepest = command.MakeCommand[*exampleSettings](
			subSubName, subSubSynonpis, subSubUsage,
			subSubExecute,
			command.WithUsageOutput(output),
		)
		subCommand = command.MakeCommand[*exampleSettings](
			subName, subSynonpis, subUsage,
			subExecute,
			command.WithSubcommands(deepest),
			command.WithUsageOutput(output),
		)
		main = command.MakeCommand[*command.HelpArg](
			cmdName, cmdSynonpis, cmdUsage,
			mainExecute,
			command.WithSubcommands(subCommand),
			command.WithUsageOutput(output),
		)
		ctx = context.TODO()
	)
	// Arguments should come from `os.Args[1:]`
	// specifying flags like this wouldn't be common.
	main.Execute(ctx, "not a sub")
	main.Execute(ctx, subName, "some args")
	main.Execute(ctx, subName, "-"+someFlagName+"=true")
	main.Execute(ctx, subName, "-"+someFlagName+"=true", "other args")
	main.Execute(ctx, subName, "more args")
	main.Execute(ctx, subName, subSubName, "args not allowed")
	main.Execute(ctx, subName, subSubName, "-"+someFlagName+"=true")

	// Output:
	// fooSenior usage
	//
	// Usage:
	// 	fooSenior subcommand [flags]
	//
	// Flags:
	//   -help
	//     	prints out this help text
	//
	// Subcommands:
	//   fooJunior - fooJunior synopis
	//
	// [some args]
	// got flag
	// [other args]
	// got flag
	// [more args]
	// fooJunior usage
	//
	// Usage:
	// 	fooJunior [flags]
	//
	// Flags:
	//   -help
	//     	prints out this help text
	//   -some-flag
	//     	some flag help text
	//
	// got flag
}
