package ipfs

import (
	"bytes"
	"io"
	"io/fs"

	intp "github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/internal"
	cbor "github.com/ipfs/go-ipld-cbor"
)

type cborFile struct {
	reader io.ReadSeeker
	node   *cbor.Node
	info   intp.NodeInfo
}

func (cio *cborFile) Close() error { return nil }

func (cio *cborFile) Stat() (fs.FileInfo, error)    { return &cio.info, nil }
func (cio *cborFile) Read(buff []byte) (int, error) { return cio.reader.Read(buff) }

func (cio *cborFile) Seek(offset int64, whence int) (int64, error) {
	return cio.reader.Seek(offset, whence)
}

func openCborFile(cborNode *cbor.Node, info *intp.NodeInfo) *cborFile {
	return &cborFile{
		node:   cborNode,
		reader: bytes.NewReader(cborNode.RawData()),
		info:   *info,
	}
}
