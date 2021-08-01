// Package errors wraps the fserrors, categorizing them in a way that allows
// translation between fs error value and host API error value.
package errors

import (
	"errors"
	"fmt"

	fserrors "github.com/ipfs/go-ipfs/filesystem/errors"
)

// Err implements the filesystem error interface.
// it is expected that all of our `filesystem.Interface` methods return these exclusively
// rather than plain Go errors. As the API wrappers depend on these error groupings.
type Error struct {
	Cause error
	Type  fserrors.Kind
}

func (e *Error) Error() string       { return e.Cause.Error() }
func (e *Error) Kind() fserrors.Kind { return e.Type }

var (
	errExist    = errors.New("already exists")
	errNotExist = errors.New("does not exist")
	errIsDir    = errors.New("not a file")
	errNotDir   = errors.New("not a directory")
	errNotEmpty = errors.New("directory is not empty")
	errReadOnly = errors.New("read only system")
)

func Exist(name string) error {
	return &Error{
		Cause: fmt.Errorf("%w: %s", errExist, name),
		Type:  fserrors.Exist,
	}
}

func NotExist(name string) error {
	return &Error{
		Cause: fmt.Errorf("%w: %s", errNotExist, name),
		Type:  fserrors.NotExist,
	}
}

func IsDir(name string) error {
	return &Error{
		Cause: fmt.Errorf("%w: %s", errIsDir, name),
		Type:  fserrors.IsDir,
	}
}

func NotDir(name string) error {
	return &Error{
		Cause: fmt.Errorf("%w: %s", errNotDir, name),
		Type:  fserrors.NotDir,
	}
}

func NotEmpty(name string) error {
	return &Error{
		Cause: fmt.Errorf("%w: %s", errNotEmpty, name),
		Type:  fserrors.NotEmpty,
	}
}

func ReadOnly(name string) error {
	return &Error{
		Cause: fmt.Errorf("%w: %s", errReadOnly, name),
		Type:  fserrors.ReadOnly,
	}
}

func UnsupportedItem(name string, err error) error {
	return &Error{
		Cause: fmt.Errorf("%w: %s", err, name),
		Type:  fserrors.InvalidItem,
	}
}

func Permission(name string, err error) error {
	return &Error{
		Cause: fmt.Errorf("%w: %s", err, name),
		Type:  fserrors.Permission,
	}
}

func Other(name string, err error) error {
	return &Error{
		Cause: fmt.Errorf("%w: %s", err, name),
		Type:  fserrors.Other,
	}
}

func IO(name string, err error) error {
	return &Error{
		Cause: fmt.Errorf("%w: %s", err, name),
		Type:  fserrors.IO,
	}
}

// TODO: [maybe]
// which system is the error talking about?
// should we take in filesystem.ID? string name of fs?
func UnsupportedRequest() error {
	return &Error{
		Cause: errors.New("operation not supported by this system"),
		Type:  fserrors.InvalidOperation,
	}
}
