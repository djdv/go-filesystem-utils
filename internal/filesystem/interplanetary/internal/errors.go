package interplanetary

import (
	"errors"
	"fmt"
	"strings"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/ipfs/boxo/path/resolver"
	ipfscmds "github.com/ipfs/go-ipfs-cmds"
)

const (
	ErrUnexpectedType = generic.ConstError("unexpected type")
	ErrEmptyLink      = generic.ConstError("empty link target")
	ErrRootLink       = generic.ConstError("root is not a symlink")
)

// TODO: review;
func ResolveErrKind(err error) fserrors.Kind {
	var resolveErr resolver.ErrNoLink
	if errors.As(err, &resolveErr) {
		return fserrors.NotExist
	}
	// XXX: Upstream doesn't define error values
	// to compare against. We have to fallback to strings.
	// This could break at any time.
	//
	// TODO: split this function?
	// 1 for errors returned from core API,
	// 1 for ipld only?
	const (
		notFoundCore = "no link named"
		// Specifically for OS sidecar files
		// that will not be valid CIDs.
		// E.g. `/$ns/desktop.ini`, `/$ns/.DS_Store`, et al.
		invalid = "invalid path"
	)
	var cmdsErr *ipfscmds.Error
	if errors.As(err, &cmdsErr) &&
		cmdsErr.Code == ipfscmds.ErrNormal &&
		(strings.Contains(cmdsErr.Message, notFoundCore) ||
			strings.Contains(cmdsErr.Message, invalid)) {
		return fserrors.NotExist
	}
	const notFoundIPLD = "no link by that name"
	if strings.Contains(err.Error(), notFoundIPLD) {
		return fserrors.NotExist
	}
	return fserrors.IO
}

func LinkLimitError(op, name string, limit uint) error {
	const kind = fserrors.Recursion
	err := fmt.Errorf(
		"reached symbolic link resolution limit (%d) during operation",
		limit,
	)
	return fserrors.New(op, name, err, kind)
}
