package ipfs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"

	"github.com/djdv/go-filesystem-utils/filesystem/errors"
	files "github.com/ipfs/go-ipfs-files"
	cbor "github.com/ipfs/go-ipld-cbor"
	ipld "github.com/ipfs/go-ipld-format"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	corepath "github.com/ipfs/interface-go-ipfs-core/path"
)

type coreFile struct {
	stat   statFunc
	f      files.File
	cancel context.CancelFunc
}

//TODO: [port]
//func (cio *coreFile) Write(_ []byte) (int, error)   { return 0, errReadOnly }
//func (cio *coreFile) Truncate(_ uint64) error       { return errReadOnly }

func (cio *coreFile) Close() error                  { defer cio.cancel(); return cio.f.Close() }
func (cio *coreFile) Stat() (fs.FileInfo, error)    { return cio.stat() }
func (cio *coreFile) Read(buff []byte) (int, error) { return cio.f.Read(buff) }
func (cio *coreFile) Size() (int64, error)          { return cio.f.Size() }
func (cio *coreFile) Seek(offset int64, whence int) (int64, error) {
	return cio.f.Seek(offset, whence)
}

type cborFile struct {
	stat   statFunc
	node   *cbor.Node
	reader io.ReadSeeker
}

//TODO: [port]
//func (cio *cborFile) Write(_ []byte) (int, error)   { return 0, errReadOnly }
//func (cio *cborFile) Truncate(_ uint64) error       { return errReadOnly }

func (cio *cborFile) Close() error                  { return nil }
func (cio *cborFile) Stat() (fs.FileInfo, error)    { return cio.stat() }
func (cio *cborFile) Read(buff []byte) (int, error) { return cio.reader.Read(buff) }
func (cio *cborFile) Size() (int64, error) {
	size, err := cio.node.Size()
	return int64(size), err
}
func (cio *cborFile) Seek(offset int64, whence int) (int64, error) {
	return cio.reader.Seek(offset, whence)
}

func openIPFSFile(ctx context.Context,
	core coreiface.CoreAPI, ipldNode ipld.Node, statFn statFunc) (fs.File, error) {
	// TODO: cborNodes should have context rules
	if cborNode, ok := ipldNode.(*cbor.Node); ok {
		// TODO: we need to pipe through the formatting bool, or use a global const
		// (or just remove it altogether and always return the raw binary data)
		humanize := true
		return openCborNode(cborNode, statFn, humanize)
	}

	corePath := corepath.IpfsPath(ipldNode.Cid())
	return openUFSNode(ctx, core, corePath, statFn)
}

func openUFSNode(ctx context.Context,
	core coreiface.CoreAPI, path corepath.Resolved, stat statFunc) (fs.File, error) {
	// TODO: double check context
	// we might want to associate cancelFuncs with handles
	// guaranteeing this exits when the file is closed (not only when the FS is destroyed)
	ctx, cancel := context.WithCancel(ctx)
	apiNode, err := core.Unixfs().Get(ctx, path)
	if err != nil {
		return nil, err
	}

	fileNode, ok := apiNode.(files.File)
	if !ok {
		// TODO: make sure caller inspects our error value
		// We should return a unique standard error that they can .Is() against
		// So that proper error values can be used with the host
		// EISDIR, etc.
		return nil, errors.New(
			errors.IsDir,
			fmt.Errorf("Unexpected node type: %T (wanted: %T)",
				apiNode, (*files.File)(nil),
				// FIXME: This message lies; the type we expect is not a pointer
			),
		)
	}

	return &coreFile{stat: stat, f: fileNode, cancel: cancel}, nil
}

func openCborNode(cborNode *cbor.Node, stat statFunc,
	formatData bool) (fs.File, error) {
	var br *bytes.Reader
	if formatData {
		forHumans, err := cborNode.MarshalJSON()
		if err != nil {
			return nil, err // TODO: errors.New
		}
		br = bytes.NewReader(forHumans)
	} else {
		br = bytes.NewReader(cborNode.RawData())
	}

	return &cborFile{stat: stat, node: cborNode, reader: br}, nil
}
