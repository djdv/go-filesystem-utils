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

const (
	success = iota
	failure
	misuse
)

type settings struct {
	command.HelpArg
}

// BindFlags defines settings flags in the [flag.FlagSet].
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

// commandName will normalize argv[0] to the program's name.
// (No absolute path, no binary file extension, etc.)
func commandName() string {
	execName := filepath.Base(os.Args[0])
	return strings.TrimSuffix(
		execName,
		filepath.Ext(execName),
	)
}

// execute is the root [command.CommandFunc]
// and expects to be called with subcommand args.
func execute(context.Context, *settings, ...string) error {
	return command.ErrUsage
}

// makeSubcommands returns a set of subcommands.
func makeSubcommands() []command.Command {
	return []command.Command{
		commands.Daemon(),
		commands.Shutdown(),
		commands.Mount(),
		commands.Unmount(),
	}
}

func exitWithErr(err error) {
	const (
		success = iota
		failure
		misuse
	)
	var (
		code     int
		printErr = func() {
			errStr := err.Error()
			if !strings.HasSuffix(errStr, "\n") {
				errStr += "\n"
			}
			os.Stderr.WriteString(errStr)
		}
	)
	if errors.Is(err, command.ErrUsage) {
		code = misuse
		// Only print these errors if they've been wrapped.
		if errors.Unwrap(err) != nil {
			printErr()
		}
	} else {
		code = failure
		printErr()
	}
	os.Exit(code)
}
