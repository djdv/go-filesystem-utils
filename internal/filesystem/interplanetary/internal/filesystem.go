package interplanetary

import (
	"io/fs"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
)

func ValidatePath(op, name string) error {
	if fs.ValidPath(name) {
		return nil
	}
	return fserrors.New(op, name, fs.ErrInvalid, fserrors.InvalidItem)
}
