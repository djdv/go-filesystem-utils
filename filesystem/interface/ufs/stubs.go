package ufs

import (
	"github.com/ipfs/go-ipfs/filesystem"
	iferrors "github.com/ipfs/go-ipfs/filesystem/interface/errors"
)

// TODO: we can implement directories, but currently have no use for them here
func (*ufsInterface) OpenDirectory(string) (filesystem.Directory, error) {
	return nil, iferrors.UnsupportedRequest()
}
func (*ufsInterface) Make(string) error            { return iferrors.UnsupportedRequest() }
func (*ufsInterface) MakeDirectory(string) error   { return iferrors.UnsupportedRequest() }
func (*ufsInterface) MakeLink(_, _ string) error   { return iferrors.UnsupportedRequest() }
func (*ufsInterface) Remove(string) error          { return iferrors.UnsupportedRequest() }
func (*ufsInterface) RemoveDirectory(string) error { return iferrors.UnsupportedRequest() }
func (*ufsInterface) RemoveLink(string) error      { return iferrors.UnsupportedRequest() }
func (*ufsInterface) Rename(_, _ string) error     { return iferrors.UnsupportedRequest() }
