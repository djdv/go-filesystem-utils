//go:build noipfs

package commands

import (
	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

func makeIPFSCommands[
	hI hostPtr[hT],
	hT any,
](filesystem.Host,
) []command.Command {
	return nil
}
