package command_test

import (
	"context"
	"fmt"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/command"
)

// MakeNiladicCommand can be used to construct
// basic commands that don't expect additional
// parameters to be passed to their execute function.
func ExampleMakeNiladicCommand() {
	var (
		cmd = newNiladicCommand()
		ctx = context.TODO()
	)
	if err := cmd.Execute(ctx); err != nil {
		fmt.Fprint(os.Stderr, err)
		return
	}
	// Output:
	// hello!
}

func newNiladicCommand() command.Command {
	const (
		name     = "niladic"
		synopsis = "Prints a message."
		usage    = "Call the command with no arguments"
	)
	return command.MakeNiladicCommand(
		name, synopsis, usage,
		niladicExecute,
	)
}

func niladicExecute(context.Context) error {
	fmt.Println("hello!")
	return nil
}
