//go:build nonfs

package commands

import "github.com/djdv/go-filesystem-utils/internal/command"

func makeNFSHostCommand() command.Command { return nil }
