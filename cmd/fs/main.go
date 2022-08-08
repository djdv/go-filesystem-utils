// Command fs interfaces with the host OS and other types of file system APIs.
package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/command"
)

type settings struct {
	command.HelpArg
	somethingDifferent int
}

func (set *settings) BindFlags(fs *flag.FlagSet) {
	set.HelpArg.BindFlags(fs)
}

func main() {
	const (
		synopsis = "File system service utility."
		usage    = "Currently doesn't do much."
	)

	var (
		cmdName     = commandName()
		cmdArgs     = os.Args[1:]
		subcommands = makeSubcommands()
		cmd         = command.MakeCommand[*settings](
			cmdName, synopsis, usage,
			execute,
			command.WithSubcommands(subcommands...),
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

func makeSubcommands() []command.Command {
	// TODO: placeholder lint.
	noop := func(context.Context, *settings, ...string) error { return nil }
	return []command.Command{
		command.MakeCommand[*settings]("subber",
			"It's a subcommand.", "You can like call it.",
			noop,
		),
		command.MakeCommand[*settings]("another",
			"Another subcommand.", "I can eat glass, it does not hurt me.",
			noop,
		),
		command.MakeCommand[*settings]("bottom_text",
			"Lorem generator", "Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. ",
			noop,
		),
	}
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
