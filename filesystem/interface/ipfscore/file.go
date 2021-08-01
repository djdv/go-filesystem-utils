package ipfscore

import (
	"bytes"
	"fmt"
	"io"

	files "github.com/ipfs/go-ipfs-files"
	"github.com/ipfs/go-ipfs/filesystem"
	interfaceutils "github.com/ipfs/go-ipfs/filesystem/interface"
	iferrors "github.com/ipfs/go-ipfs/filesystem/interface/errors"
	cbor "github.com/ipfs/go-ipld-cbor"
)

var _, _ filesystem.File = (*coreFile)(nil), (*cborFile)(nil)

type coreFile struct{ f files.File }

func (cio *coreFile) Size() (int64, error)          { return cio.f.Size() }
func (cio *coreFile) Read(buff []byte) (int, error) { return cio.f.Read(buff) }
func (cio *coreFile) Write(_ []byte) (int, error)   { return 0, errReadOnly }
func (cio *coreFile) Truncate(_ uint64) error       { return errReadOnly }
func (cio *coreFile) Close() error                  { return cio.f.Close() }
func (cio *coreFile) Seek(offset int64, whence int) (int64, error) {
	return cio.f.Seek(offset, whence)
}

type cborFile struct {
	node   *cbor.Node
	reader io.ReadSeeker
}

func (cio *cborFile) Size() (int64, error) {
	size, err := cio.node.Size()
	return int64(size), err
}
func (cio *cborFile) Read(buff []byte) (int, error) { return cio.reader.Read(buff) }
func (cio *cborFile) Write(_ []byte) (int, error)   { return 0, errReadOnly }
func (cio *cborFile) Truncate(_ uint64) error       { return errReadOnly }
func (cio *cborFile) Close() error                  { return nil }
func (cio *cborFile) Seek(offset int64, whence int) (int64, error) {
	return cio.reader.Seek(offset, whence)
}

func (ci *coreInterface) Open(path string, flags filesystem.IOFlags) (filesystem.File, error) {
	if flags != filesystem.IOReadOnly {
		return nil, iferrors.ReadOnly(path)
	}

	corePath := ci.joinRoot(path)

	callCtx, callCancel := interfaceutils.CallContext(ci.ctx)
	defer callCancel()
	ipldNode, err := ci.core.ResolveNode(callCtx, corePath)
	if err != nil {
		return nil, iferrors.Permission(path, err)
	}

	// special handling for cbor nodes
	if cborNode, ok := ipldNode.(*cbor.Node); ok {
		br := bytes.NewReader(cborNode.RawData())
		return &cborFile{node: cborNode, reader: br}, nil
		// TODO [review] we could return this as human readable JSON instead of the raw data
		// but I'm not sure which is prefferable
		/*
			forHumans, err := cborNode.MarshalJSON()
			if err != nil {
				return nil, err
			}
			br := bytes.NewReader(forHumans)
		*/
	}

	apiNode, err := ci.core.Unixfs().Get(ci.ctx, corePath)
	if err != nil {
		return nil, iferrors.Permission(path, err)
	}

	fileNode, ok := apiNode.(files.File)
	if !ok {
		return nil, fmt.Errorf("(Type: %v), %w",
			apiNode,
			iferrors.IsDir(path),
		)
	}

	return &coreFile{f: fileNode}, nil
}
