package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/commands"
)

type settings struct {
	command.HelpFlag
}

func (set *settings) BindFlags(fs *flag.FlagSet) {
	command.NewHelpFlag(fs, &set.HelpFlag)
}

func main() {
	const (
		synopsis = "9P prototype utility."
		usage    = "Placeholder text. Call the subcommands."
	)

	var (
		cmdName = commandName()
		cmdArgs = os.Args[1:]
		cmd     = command.MakeCommand(
			cmdName, synopsis, usage,
			execute,
			command.WithSubcommands(
				commands.Daemon(),
				commands.List(),
				commands.Read(),
				commands.Write(),
			),
		)
		ctx = context.Background()
	)

	if err := cmd.Execute(ctx, cmdArgs...); err != nil {
		exitWithErr(err)
	}
}

// commandName will normalize argv[0] to the program's name (only).
// (No absolute path, no binary file extension, etc.)
func commandName() string {
	execName := filepath.Base(os.Args[0])
	return strings.TrimSuffix(
		execName,
		filepath.Ext(execName),
	)
}

func execute(context.Context, *settings, ...string) error {
	// The root command only holds subcommands
	// and has no functionality on its own.
	return command.ErrUsage
}

func exitWithErr(err error) {
	const (
		success = iota
		failure
		misuse
	)
	var code int
	if errors.Is(err, command.ErrUsage) {
		code = misuse
	} else {
		code = failure
		os.Stderr.WriteString(err.Error())
	}
	os.Exit(code)
}
