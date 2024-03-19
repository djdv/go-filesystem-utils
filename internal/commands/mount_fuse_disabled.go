//go:build nofuse

package commands

import (
	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

const fuseID filesystem.Host = ""

func makeFUSECommand() command.Command { return nil }
