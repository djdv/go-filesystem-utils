//go:build go1.21

package commands

import (
	"slices"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/command"
)

func sortCommands(commands []command.Command) {
	slices.SortFunc(
		commands,
		func(a, b command.Command) int {
			return strings.Compare(
				a.Name(),
				b.Name(),
			)
		},
	)
}
