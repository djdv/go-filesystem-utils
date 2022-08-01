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
	"github.com/djdv/go-filesystem-utils/internal/commands"
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
	return []command.Command{
		commands.Daemon(),
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
		errStr := err.Error()
		if !strings.HasSuffix(errStr, "\n") {
			errStr += "\n"
		}
		os.Stderr.WriteString(errStr)
	}
	os.Exit(code)
}
