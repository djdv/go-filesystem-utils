package mfs

import (
	"errors"
	"fmt"
	"os"

	"github.com/ipfs/go-ipfs/filesystem"
	iferrors "github.com/ipfs/go-ipfs/filesystem/interface/errors"
	gomfs "github.com/ipfs/go-mfs"
)

var _ filesystem.File = (*mfsIOWrapper)(nil)

type mfsIOWrapper struct{ f gomfs.FileDescriptor }

func (mio *mfsIOWrapper) Size() (int64, error)           { return mio.f.Size() }
func (mio *mfsIOWrapper) Read(buff []byte) (int, error)  { return mio.f.Read(buff) }
func (mio *mfsIOWrapper) Write(buff []byte) (int, error) { return mio.f.Write(buff) }
func (mio *mfsIOWrapper) Truncate(size uint64) error     { return mio.f.Truncate(int64(size)) }
func (mio *mfsIOWrapper) Close() error                   { return mio.f.Close() }
func (mio *mfsIOWrapper) Seek(offset int64, whence int) (int64, error) {
	return mio.f.Seek(offset, whence)
}

func (mi *mfsInterface) Open(path string, flags filesystem.IOFlags) (filesystem.File, error) {
	mfsNode, err := gomfs.Lookup(mi.mroot, path)
	if err != nil {
		return nil, mfsLookupErr(path, err)
		if errors.Is(err, os.ErrNotExist) {
			return nil, iferrors.NotExist(path)
		}
		return nil, iferrors.Permission(path, err)
	}

	mfsFileIf, ok := mfsNode.(*gomfs.File)
	if !ok {
		return nil, fmt.Errorf("(Type: %v), %w",
			mfsNode.Type(),
			iferrors.IsDir(path),
		)
	}

	mfsFile, err := mfsFileIf.Open(translateFlags(flags))
	if err != nil {
		return nil, iferrors.Permission(path, err)
	}

	return &mfsIOWrapper{f: mfsFile}, nil
}

func translateFlags(flags filesystem.IOFlags) gomfs.Flags {
	switch flags {
	case filesystem.IOReadOnly:
		return gomfs.Flags{Read: true}
	case filesystem.IOWriteOnly:
		return gomfs.Flags{Write: true}
	case filesystem.IOReadWrite:
		return gomfs.Flags{Read: true, Write: true}
	default:
		return gomfs.Flags{}
	}
}
