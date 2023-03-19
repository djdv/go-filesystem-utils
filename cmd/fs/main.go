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

func main() {
	const (
		synopsis = "File system service utility."
	)
	var (
		name        = commandName()
		arguments   = os.Args[1:]
		subcommands = makeSubcommands()
		ctx         = context.Background()
		err         = command.SubcommandGroup(
			name, synopsis,
			subcommands,
		).Execute(ctx, arguments...)
	)
	if err != nil {
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
	if errors.Is(err, flag.ErrHelp) {
		// We must exit with the correct code,
		// but don't need to print this error itself.
		// The command library will have already printed
		// the usage text (as requested).
		os.Exit(misuse)
	}
	var (
		code     int
		usageErr command.UsageError
	)
	if errors.As(err, &usageErr) {
		// Inappropriate input.
		code = misuse
	} else {
		// Operation failure.
		code = failure
	}
	errStr := err.Error()
	if !strings.HasSuffix(errStr, "\n") {
		errStr += "\n"
	}
	os.Stderr.WriteString(errStr)
	os.Exit(code)
}

func isWrapped(err error) bool {
	if errors.Unwrap(err) != nil {
		return true
	}
	joinErrs, ok := err.(interface {
		Unwrap() []error
	})
	if ok {
		return len(joinErrs.Unwrap()) > 1
	}
	return false
}
