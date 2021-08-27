package mfs

import (
	"context"
	goerrors "errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/filesystem/errors"
	"github.com/ipfs/go-cid"
	dag "github.com/ipfs/go-merkledag"
	"github.com/ipfs/go-mfs"
	"github.com/ipfs/go-unixfs"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

const rootName = "."

type mfsInterface struct {
	ctx          context.Context
	mroot        *mfs.Root
	creationTime time.Time
}

// TODO: this should probably not be exported; here for testing purposes
func NewRoot(ctx context.Context, core coreiface.CoreAPI) (*mfs.Root, error) {
	return CidToMFSRoot(ctx, unixfs.EmptyDirNode().Cid(), core, nil)
}

// TODO: we should probably not export this, and instead use options on the NewInterface constructor
// E.g. `WithMFSRoot(mroot)`, `WithCID(cid)`, or none which uses an empty directory by default.
func CidToMFSRoot(ctx context.Context, rootCid cid.Cid, core coreiface.CoreAPI, publish mfs.PubFunc) (*mfs.Root, error) {
	if !rootCid.Defined() {
		return nil, errors.New("root cid was not defined")
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ipldNode, err := core.Dag().Get(callCtx, rootCid)
	if err != nil {
		return nil, err
	}
	pbNode, ok := ipldNode.(*dag.ProtoNode)
	if !ok {
		return nil, fmt.Errorf("%q has incompatible type %T", rootCid.String(), ipldNode)
	}

	return mfs.NewRoot(ctx, core.Dag(), pbNode, publish)
}

func NewInterface(ctx context.Context, mroot *mfs.Root) fs.FS {
	if mroot == nil {
		panic(fmt.Errorf("MFS root was not provided"))
	}
	return &mfsInterface{
		ctx:          ctx,
		mroot:        mroot,
		creationTime: time.Now(),
	}
}

func (mi *mfsInterface) ID() filesystem.ID { return filesystem.MFS }
func (mi *mfsInterface) Close() error      { return mi.mroot.Close() }

// TODO move this
func (mi *mfsInterface) Rename(oldName, newName string) error {
	const op errors.Op = "mfs.Rename"
	if err := mfs.Mv(mi.mroot, oldName, newName); err != nil {
		return errors.New(op,
			errors.Path(oldName+" -> "+newName), // TODO support multiple paths in New, we can switch off op suffix or a real type in Error() fmt
			errors.IO,
		)
	}
	return nil
}

func (mi *mfsInterface) Open(name string) (fs.File, error) {
	const op errors.Op = "mfs.Open"
	if name == rootName {
		return mi.OpenDir(name)
	}

	if !fs.ValidPath(name) {
		// TODO: [pkg-wide] We're supposed to return fs.PathErrors in these functions.
		// We'll have to embedd these into them and then unwrap them where we expect them.
		// (ourErrToPOSIXErrno, etc.)
		return nil, errors.New(op,
			errors.Path(name),
			errors.InvalidItem,
		)
	}

	mfsNode, err := mfs.Lookup(mi.mroot, path.Join("/", name))
	if err != nil {
		log.Println("hit1:", path.Join("/", name))
		if goerrors.Is(err, os.ErrNotExist) {
			return nil, errors.New(op,
				errors.Path(name),
				errors.NotExist,
			)
		}
		return nil, errors.New(op,
			errors.Path(name),
			errors.Permission,
		)
	}

	switch mfsIntf := mfsNode.(type) {
	case *mfs.File:
		flags := mfs.Flags{Read: true}
		mfsFileIntf, err := mfsIntf.Open(flags)
		if err != nil {
			return nil, errors.New(op,
				errors.Path(name),
				errors.Permission,
			)
		}
		mStat := &mfsStat{
			creationTime: mi.creationTime,
			name:         path.Base(name),
			mode:         fs.FileMode(0),
			size:         mfsFileIntf.Size,
		}

		return &mfsFile{f: mfsFileIntf, stat: mStat}, nil

	case *mfs.Directory:
		// TODO: split up opendir and call that here
		// e.g. mi.openDir(mfsNode) or something more optimal than full reparse
		return mi.OpenDir(name)

	default:
		return nil, errors.New(op,
			errors.Path(name),
			errors.Permission,
		)
	}
}
