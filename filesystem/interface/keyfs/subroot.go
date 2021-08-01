package keyfs

import (
	"fmt"
	"io"

	"github.com/ipfs/go-ipfs/filesystem"
	interfaceutils "github.com/ipfs/go-ipfs/filesystem/interface"
	iferrors "github.com/ipfs/go-ipfs/filesystem/interface/errors"
	"github.com/ipfs/go-ipfs/filesystem/interface/mfs"
	"github.com/ipfs/go-merkledag"
	gomfs "github.com/ipfs/go-mfs"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

// rootRef wraps a foreign file system
// with the means to manage sub-references of that system.
type rootRef struct {
	filesystem.Interface
	counter refCounter
}

// root references must be closed when no longer used
// otherwise they'll remain in the active table
func (rr rootRef) Close() error { return rr.counter.decrement() }

// sub references must also be closed when no longer used
// for the same reason
type (
	rootFileRef struct {
		filesystem.File
		io.Closer
	}
	rootDirectoryRef struct {
		filesystem.Directory
		io.Closer
	}
)

func (rf rootFileRef) Close() error      { return rf.Closer.Close() }
func (rd rootDirectoryRef) Close() error { return rd.Closer.Close() }

func rootCloserGen(rootRef *rootRef, subRef io.Closer) closer {
	return func() error {
		err := subRef.Close()               // `Close` the subreference itself
		rErr := rootRef.counter.decrement() // remove association with its superior
		if err == nil && rErr != nil {      // returning the supererror, only if there is no suberror
			err = rErr
		}
		return err
	}
}

// `Open` overrides the native system's `Open` method
// adding in reference tracking to a shared instance of the system
func (rr rootRef) Open(path string, flags filesystem.IOFlags) (filesystem.File, error) {
	rr.counter.increment()
	file, err := rr.Interface.Open(path, flags)
	if err != nil {
		rr.counter.decrement() // we know we're not the last reference so the error is unchecked
		return nil, err
	}

	return rootFileRef{
		File:   file,
		Closer: rootCloserGen(&rr, file),
	}, nil
}

func (rr rootRef) OpenDirectory(path string) (filesystem.Directory, error) {
	rr.counter.increment()
	directory, err := rr.Interface.OpenDirectory(path)
	if err != nil {
		rr.counter.decrement() // we know we're not the last reference so the error is unchecked
		return nil, err
	}

	return &rootDirectoryRef{
		Directory: directory,
		Closer:    rootCloserGen(&rr, directory),
	}, nil
}

func (ki *keyInterface) getRoot(key coreiface.Key) (filesystem.Interface, error) {
	return ki.references.getRootRef(key.Name(), func() (filesystem.Interface, error) {
		mroot, err := ki.keyToMFSRoot(key)
		if err != nil {
			return nil, err
		}

		return mfs.NewInterface(ki.ctx, mroot)
	})
}

func (ki *keyInterface) keyToMFSRoot(key coreiface.Key) (*gomfs.Root, error) {
	callCtx, cancel := interfaceutils.CallContext(ki.ctx)
	defer cancel()

	path, err := ki.core.ResolvePath(callCtx, key.Path())
	if err != nil {
		return nil, err
	}

	ipldNode, err := ki.core.ResolveNode(callCtx, path)
	if err != nil {
		return nil, err
	}

	iStat, _, err := ki.core.Stat(callCtx, path, filesystem.StatRequest{Type: true})
	if err != nil {
		return nil, err
	}

	if iStat.Type != coreiface.TDirectory {
		err := fmt.Errorf("(Type: %v), %w",
			iStat.Type,
			iferrors.NotDir(key.Name()),
		)

		return nil, err
	}

	pbNode, ok := ipldNode.(*merkledag.ProtoNode)
	if !ok {
		err := fmt.Errorf("incompatible root node type (%T)", ipldNode)
		return nil, iferrors.UnsupportedItem(key.Name(), err)
	}

	mroot, err := gomfs.NewRoot(ki.ctx, ki.core.Dag(), pbNode, ki.publisherGenMFS(key.Name()))
	if err != nil {
		return nil, iferrors.IO(key.Name(), err)
	}
	return mroot, nil
}
