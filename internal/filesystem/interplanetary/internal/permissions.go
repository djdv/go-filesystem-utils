package interplanetary

import (
	"fmt"
	"io/fs"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

const (
	ExecuteAll = filesystem.ExecuteUser | filesystem.ExecuteGroup | filesystem.ExecuteOther
	ReadAll    = filesystem.ReadUser | filesystem.ReadGroup | filesystem.ReadOther
)

func SetModePermissions(mode *fs.FileMode, permissions fs.FileMode) error {
	if got := permissions.Perm() &^ permissions; got != 0 {
		return fmt.Errorf("non-permission bits were included,"+
			"got: `%#o` wanted: within range `%#o`",
			got, fs.ModePerm,
		)
	}
	*mode = (*mode).Type() | permissions.Perm()
	return nil
}
