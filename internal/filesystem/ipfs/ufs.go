package ipfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	files "github.com/ipfs/go-ipfs-files"
	ipld "github.com/ipfs/go-ipld-format"
	unixfsfile "github.com/ipfs/go-unixfs/file"
)

type (
	ufsFile struct {
		files.File
		cancel context.CancelFunc
		info   nodeInfo
	}
)

const errUnexpectedType = generic.ConstError("unexpected type")

func openUFSFile(ctx context.Context, dag ipld.DAGService,
	node ipld.Node, stat *nodeInfo,
) (*ufsFile, error) {
	ctx, cancel := context.WithCancel(ctx)
	apiNode, err := unixfsfile.NewUnixfsFile(ctx, dag, node)
	if err != nil {
		cancel()
		return nil, err
	}
	fileNode, ok := apiNode.(files.File)
	if !ok {
		cancel()
		return nil, fmt.Errorf(
			"%w got: \"%T\" want: \"files.File\"",
			errUnexpectedType, apiNode,
		)
	}
	return &ufsFile{
		info:   *stat,
		File:   fileNode,
		cancel: cancel,
	}, nil
}

func ufsOpenErr(err error) fserrors.Kind {
	if errors.Is(err, errUnexpectedType) {
		return fserrors.IsDir
	}
	return fserrors.IO
}

func (uio *ufsFile) Close() error { defer uio.cancel(); return uio.File.Close() }

func (uio *ufsFile) Stat() (fs.FileInfo, error) { return &uio.info, nil }

func (uio *ufsFile) Seek(offset int64, whence int) (int64, error) {
	return uio.File.Seek(offset, whence)
}
