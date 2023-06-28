//go:build noipfs

package commands

import (
	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

func makeIPFSCommands[
	HC mountCmdHost[HT, HM],
	HM marshaller,
	HT any,
](filesystem.Host,
) []command.Command {
	return nil
}

func makeIPFSGuests[
	HC mountPointHost[T],
	T any,
](mountPointGuests, ninePath,
) { /* NOOP */ }
