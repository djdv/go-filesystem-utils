package command_test

import (
	"context"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/command"
)

// Subcommand groups can be useful for defining
// a section of related commands under a single
// named group.
func ExampleSubcommandGroup() {
	var (
		cmd = newSubcommands()
		ctx = context.TODO()
	)
	cmd.Execute(ctx, "-help")
	cmd.Execute(ctx, "alphabets", "-help")
	cmd.Execute(ctx, "numerals", "-help")
	// Output:
	// Must be called with a subcommand.
	//
	// Usage:
	// 	main subcommand [flags]
	//
	// Flags:
	//   -help
	//     	prints out this help text
	//
	// Subcommands:
	//   alphabets - Letter group.
	//   numerals  - Number group.
	//
	// Must be called with a subcommand.
	//
	// Usage:
	// 	alphabets subcommand [flags]
	//
	// Flags:
	//   -help
	//     	prints out this help text
	//
	// Subcommands:
	//   a - a synopsis
	//   b - b synopsis
	//   c - c synopsis
	//
	// Must be called with a subcommand.
	//
	// Usage:
	// 	numerals subcommand [flags]
	//
	// Flags:
	//   -help
	//     	prints out this help text
	//
	// Subcommands:
	//   1 - 1 synopsis
	//   2 - 2 synopsis
	//   3 - 3 synopsis
}

func newSubcommands() command.Command {
	var (
		noopFn          = func(context.Context) error { return nil }
		mustMakeCommand = func(name string) command.Command {
			var (
				synopsis = name + " synopsis"
				usage    = name + " usage"
				cmd, err = command.MakeNiladicCommand(
					name, synopsis, usage, noopFn,
				)
			)
			if err != nil {
				panic(err)
			}
			return cmd
		}
		// Printer output defaults to [os.Stderr].
		// We set it here only because `go test`
		// compares against [os.Stdout].
		output     = os.Stdout
		cmdOptions = []command.Option{
			command.WithUsageOutput(output),
		}
		cmd = command.SubcommandGroup(
			"main", "Top level group",
			[]command.Command{
				command.SubcommandGroup(
					"alphabets", "Letter group.",
					[]command.Command{
						mustMakeCommand("a"),
						mustMakeCommand("b"),
						mustMakeCommand("c"),
					},
					cmdOptions...,
				),
				command.SubcommandGroup(
					"numerals", "Number group.",
					[]command.Command{
						mustMakeCommand("1"),
						mustMakeCommand("2"),
						mustMakeCommand("3"),
					},
					cmdOptions...,
				),
			},
			cmdOptions...,
		)
	)
	return cmd
}
