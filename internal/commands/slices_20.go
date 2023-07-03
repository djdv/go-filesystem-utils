//go:build !go1.21

package commands

import (
	"github.com/djdv/go-filesystem-utils/internal/command"
	"golang.org/x/exp/slices"
)

func sortCommands(commands []command.Command) {
	slices.SortFunc(commands, func(a, b command.Command) bool {
		return a.Name() < b.Name()
	})
}
