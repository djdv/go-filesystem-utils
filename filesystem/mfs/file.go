package mfs

import (
	"io/fs"
	"time"

	"github.com/ipfs/go-mfs"
)

type mfsFile struct {
	f    mfs.FileDescriptor
	stat *mfsStat
}

func (mio *mfsFile) Read(buff []byte) (int, error) {
	return mio.f.Read(buff)
}
func (mio *mfsFile) Write(buff []byte) (int, error) { return mio.f.Write(buff) }
func (mio *mfsFile) Truncate(size uint64) error     { return mio.f.Truncate(int64(size)) }
func (mio *mfsFile) Close() error                   { return mio.f.Close() }
func (mio *mfsFile) Seek(offset int64, whence int) (int64, error) {
	return mio.f.Seek(offset, whence)
}

func (mio *mfsFile) Stat() (fs.FileInfo, error) { return mio.stat, nil }

type mfsStat struct {
	creationTime time.Time
	mode         fs.FileMode
	name         string // TODO: make sure this is updated in a .rename method on the file
	size         func() (int64, error)
}

func (ms *mfsStat) Name() string { return ms.name }
func (ms *mfsStat) Size() int64 {
	if ms.IsDir() {
		return 0
	}
	size, err := ms.size()
	if err != nil {
		panic(err) // TODO: we should log this instead and return 0
	}
	return size
}
func (ms *mfsStat) Mode() fs.FileMode  { return ms.mode }
func (ms *mfsStat) ModTime() time.Time { return ms.creationTime }
func (ms *mfsStat) IsDir() bool        { return ms.Mode().IsDir() } // [spec] Don't hardcode this.
func (ms *mfsStat) Sys() interface{}   { return ms }
