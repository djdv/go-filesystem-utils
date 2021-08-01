package keyfs

import (
	"errors"
	"io"
	"sync"

	"github.com/ipfs/go-ipfs/filesystem"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

var _ filesystem.File = (*keyFile)(nil)

// TODO: rewrite this in English; split between fileRef too
// keyFile implements the `File` interface by wrapping a (shared) ufs.`File`
// references to a `keyFile` should utilize the shared lock on their underlying `fileRef`
// during operations to prevent conflicting modifications
// the underlying reference must have its cursor adjusted to match the cursor value stored on each reference
// as this value is unique per reference while the underlying cursor position may have been modified by another caller.
type keyFile struct {
	fileRef
	cursor int64
	flags  filesystem.IOFlags // TODO: [cbcd58ed-86e1-4a2f-87ad-5598d4ea1de5]
}

type fileRef struct {
	filesystem.File
	*sync.Mutex
	counter refCounter
	io.Closer
}

// override `File.Close` with our own close method (which should in itself eventually call `File.Close`).
func (fi fileRef) Close() error { return fi.Closer.Close() }

func (ki *keyInterface) Open(path string, flags filesystem.IOFlags) (filesystem.File, error) {
	fs, key, fsPath, deferFunc, err := ki.selectFS(path)
	if err != nil {
		return nil, err
	}
	defer deferFunc()

	if fs == ki {
		return ki.getFile(key, flags)
	}

	return fs.Open(fsPath, flags)
}

func (kio *keyFile) Size() (int64, error) {
	// NOTE: this could be a read lock since Size shouldn't modify the dagmod
	// but a rwmutex doesn't seem worth it for single short op
	kio.fileRef.Lock()
	defer kio.fileRef.Unlock()
	return kio.fileRef.Size()
}

func (kio *keyFile) Read(buff []byte) (int, error) {
	kio.fileRef.Lock()
	defer kio.fileRef.Unlock()
	if _, err := kio.fileRef.Seek(kio.cursor, io.SeekStart); err != nil {
		return 0, err
	}

	readBytes, err := kio.fileRef.Read(buff)
	kio.cursor += int64(readBytes)

	return readBytes, err
}

func (kio *keyFile) Write(buff []byte) (int, error) {
	kio.fileRef.Lock()
	defer kio.fileRef.Unlock()

	if _, err := kio.fileRef.Seek(kio.cursor, io.SeekStart); err != nil {
		return 0, err
	}

	wroteBytes, err := kio.fileRef.Write(buff)
	kio.cursor += int64(wroteBytes)

	return wroteBytes, err
}

func (kio *keyFile) Seek(offset int64, whence int) (int64, error) {
	kio.fileRef.Lock() // NOTE: same note as in Size(); and because of call to dag.Size()
	defer kio.fileRef.Unlock()

	switch whence {
	case io.SeekStart:
		if offset < 0 {
			return kio.cursor, errors.New("tried to seek to a position before the beginning of the file")
		}
		kio.cursor = offset
	case io.SeekCurrent:
		kio.cursor += offset
	case io.SeekEnd:
		end, err := kio.fileRef.Size()
		if err != nil {
			return kio.cursor, err
		}
		kio.cursor = end + offset
	}

	// NOTE: this seek isn't actually meaningful outside of validating the offset
	return kio.fileRef.Seek(kio.cursor, io.SeekStart)
}

func (kio *keyFile) Truncate(size uint64) error {
	kio.fileRef.Lock()
	defer kio.fileRef.Unlock()
	return kio.fileRef.Truncate(size)
}

// getFile will either construct a `File` representation of the key
// or fetch an existing one from a table of shared references
// (handling reference count internally/automatically via keyFile's `Close` method)

func (ki *keyInterface) getFile(key coreiface.Key, flags filesystem.IOFlags) (filesystem.File, error) {
	// TODO: [cbcd58ed-86e1-4a2f-87ad-5598d4ea1de5]
	// the `File`s we `Open` should always have full access
	// but `keyFile` references that are returned should store the provided flags
	// and gate operations based on it
	// e.g. keyFile.Write(){if !self.flags.write; return errRO}
	// right now we don't do this, we just store them

	keyName := key.Name()
	opener := func() (filesystem.File, error) {
		ki.ufs.SetModifier(ki.publisherGenUFS(keyName))
		return ki.ufs.Open(key.Path().String(), filesystem.IOReadWrite)
	}

	fileRef, err := ki.references.getFileRef(key.Name(), opener)
	if err != nil {
		return nil, err
	}

	// return a wrapper around it with a unique cursor and flagset
	return &keyFile{fileRef: fileRef, flags: flags}, nil
}
