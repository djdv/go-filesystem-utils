// Command fs interfaces with the host OS and other types of file system APIs.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/cmds/fs"
	"github.com/ipfs/go-ipfs-cmds/cli"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handleCmdsErr(cmdsRun(ctx))
}

func cmdsRun(ctx context.Context) error {
	return cli.Run(ctx,
		fs.Command(), cmdsArgs(os.Args),
		os.Stdin, os.Stdout, os.Stderr,
		nil, nil,
	)
}

// The cmds helptext generator uses its `cmdline[0]` literally.
// So we normalize argv[0] to the program's name (only).
// (No absolute path, no binary file extension, etc.)
func cmdsArgs(argv []string) []string {
	var (
		ourName    = filepath.Base(argv[0])
		formalName = strings.TrimSuffix(ourName, filepath.Ext(ourName))
	)
	return append([]string{formalName}, os.Args[1:]...)
}

func handleCmdsErr(err error) {
	// NOTE: We only check for `cli` exit codes.
	// The cmdslib will have already printed to stderr
	// so we disregard the string form.
	var cliError cli.ExitError
	if errors.As(err, &cliError) {
		os.Exit(int(cliError))
	}
}
