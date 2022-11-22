package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"runtime"
	"time"

	// TODO: migrate to new standard
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/ipfs/go-cid"
	dag "github.com/ipfs/go-merkledag"
	"github.com/ipfs/go-mfs"
	"github.com/ipfs/go-unixfs"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

type (
	mfsInterface struct {
		creationTime time.Time
		ctx          context.Context
		mroot        *mfs.Root
	}
	mfsDirectory struct {
		ctx    context.Context
		cancel context.CancelFunc
		stat   rootStat
		mfsDir *mfs.Directory
		nodes  []mfs.NodeListing
	}
	mfsDirEntry struct {
		creationTime time.Time
		node         mfs.NodeListing
	}
	entryStat struct {
		creationTime time.Time
		node         mfs.NodeListing
	}
	mfsFile struct {
		f    mfs.FileDescriptor
		stat *mfsStat
	}
	mfsStat struct {
		creationTime time.Time
		size         func() (int64, error)
		name         string // TODO: make sure this is updated in a .rename method on the file
		mode         fs.FileMode
	}
)

// TODO: this should probably not be exported; here for testing purposes
/*
func NewRoot(ctx context.Context, core coreiface.CoreAPI) (*mfs.Root, error) {
	return CidToMFSRoot(ctx, unixfs.EmptyDirNode().Cid(), core, nil)
}
*/

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

func NewMFS(ctx context.Context, mroot *mfs.Root) fs.FS {
	if mroot == nil {
		panic(fmt.Errorf("MFS root was not provided"))
	}
	return &mfsInterface{
		ctx:          ctx,
		mroot:        mroot,
		creationTime: time.Now(),
	}
}

func (mi *mfsInterface) ID() ID       { return MFS }
func (mi *mfsInterface) Close() error { return mi.mroot.Close() }

// TODO move this
func (mi *mfsInterface) Rename(oldName, newName string) error {
	const op fserrors.Op = "mfs.Rename"
	if err := mfs.Mv(mi.mroot, oldName, newName); err != nil {
		return fserrors.New(op,
			fserrors.Path(oldName+" -> "+newName), // TODO support multiple paths in New, we can switch off op suffix or a real type in Error() fmt
			fserrors.IO,
		)
	}
	return nil
}

func (mi *mfsInterface) Open(name string) (fs.File, error) {
	const op fserrors.Op = "mfs.Open"
	if name == rootName {
		return mi.OpenDir(name)
	}

	if !fs.ValidPath(name) {
		// TODO: [pkg-wide] We're supposed to return fs.PathErrors in these functions.
		// We'll have to embedd these into them and then unwrap them where we expect them.
		// (ourErrToPOSIXErrno, etc.)
		return nil, fserrors.New(op,
			fserrors.Path(name),
			fserrors.InvalidItem,
		)
	}

	mfsNode, err := mfs.Lookup(mi.mroot, path.Join("/", name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fserrors.New(op,
				fserrors.Path(name),
				fserrors.NotExist,
			)
		}
		log.Println("hit1:", path.Join("/", name))
		log.Println("dumping callstack")
		var i int
		for {
			pc, fn, line, ok := runtime.Caller(i)
			if !ok {
				break
			}
			log.Printf("/!\\ [sf{%d}] %s[%s:%d]\n", i, runtime.FuncForPC(pc).Name(), fn, line)
			i++
		}
		return nil, fserrors.New(op,
			fserrors.Path(name),
			fserrors.Permission,
		)
	}

	switch mfsIntf := mfsNode.(type) {
	case *mfs.File:
		flags := mfs.Flags{Read: true}
		mfsFileIntf, err := mfsIntf.Open(flags)
		if err != nil {
			log.Println("hit2:", path.Join("/", name))
			log.Println("dumping callstack")
			var i int
			for {
				pc, fn, line, ok := runtime.Caller(i)
				if !ok {
					break
				}
				log.Printf("/!\\ [sf{%d}] %s[%s:%d]\n", i, runtime.FuncForPC(pc).Name(), fn, line)
				i++
			}
			return nil, fserrors.New(op,
				fserrors.Path(name),
				fserrors.Permission,
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
		log.Println("hit3:", path.Join("/", name))
		log.Println("dumping callstack")
		var i int
		for {
			pc, fn, line, ok := runtime.Caller(i)
			if !ok {
				break
			}
			log.Printf("/!\\ [sf{%d}] %s[%s:%d]\n", i, runtime.FuncForPC(pc).Name(), fn, line)
			i++
		}
		return nil, fserrors.New(op,
			fserrors.Path(name),
			fserrors.Permission,
		)
	}
}

func (md *mfsDirectory) Stat() (fs.FileInfo, error) { return &md.stat, nil }

func (*mfsDirectory) Read([]byte) (int, error) {
	const op fserrors.Op = "mfsDirectory.Read"
	return -1, fserrors.New(op, fserrors.IsDir)
}

func (mi *mfsInterface) OpenDir(name string) (fs.ReadDirFile, error) {
	const op fserrors.Op = "mfs.OpenDir"
	var (
		mfsNode mfs.FSNode
		err     error
	)
	if name == rootName {
		mfsNode, err = mfs.Lookup(mi.mroot, "/")
	} else {
		mfsNode, err = mfs.Lookup(mi.mroot, path.Join("/", name))
	}
	if err != nil {
		return nil, fserrors.New(op,
			fserrors.Path(name),
			err,
		)
	}

	mfsDir, isDir := mfsNode.(*mfs.Directory)
	if !isDir {
		return nil, fserrors.New(op,
			fserrors.Path(name),
			fmt.Errorf("type %v != %v (directory)",
				mfsNode.Type(),
				mfs.TDir),
			fserrors.NotDir,
		)
	}

	ctx, cancel := context.WithCancel(mi.ctx)
	return &mfsDirectory{
		ctx: ctx, cancel: cancel,
		// stat:   (*rootStat)(&mi.creationTime),
		stat:   newRootStat(s_IRWXA), // TODO: permissions from caller.
		mfsDir: mfsDir,
	}, nil
}

func (md *mfsDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	const op fserrors.Op = "mfsDirectory.ReadDir"
	var (
		nodes     = md.nodes
		returnAll = count <= 0
	)
	if nodes == nil {
		// TODO: this should only happen in Open?
		listings, err := md.mfsDir.List(md.ctx)
		if err != nil {
			return nil, fserrors.New(op, err) // TODO we could probably add more context
		}
		nodes = listings
		md.nodes = nodes
	}

	// FIXME: offset will likely be wrong; quick port
	entries := make([]fs.DirEntry, generic.Min(count, len(nodes)))
	for _, node := range nodes {
		if count <= 0 {
			if returnAll {
				return entries, nil
			}
			return entries, io.EOF
		}
		entries = append(entries, &mfsDirEntry{
			node: node,
			// creationTime: *(*time.Time)(md.stat),
			// TODO: ^ stubbed
		})
	}
	return nil, nil // TODO: standard compliance check; quick port
	// return gofs.ReadDir(count, entries)
}

func (md *mfsDirectory) Close() error {
	const op fserrors.Op = "mfsDirectory.Close"
	if cancel := md.cancel; cancel != nil {
		md.cancel = nil
		cancel()
		return nil
	}
	return fserrors.New(op,
		fserrors.InvalidItem, // TODO: Check POSIX expected values
		"directory was not open",
	)
}

func (me *mfsDirEntry) Name() string { return me.node.Name }

func (me *mfsDirEntry) Info() (fs.FileInfo, error) {
	return &entryStat{node: me.node, creationTime: me.creationTime}, nil
}

func (me *mfsDirEntry) Type() fs.FileMode {
	info, err := me.Info()
	if err != nil {
		return fs.ModeIrregular
	}
	return info.Mode() & fs.ModeType
}

func (me *mfsDirEntry) IsDir() bool { return me.Type()&fs.ModeDir != 0 }

func (es *entryStat) Name() string       { return es.node.Name }
func (es *entryStat) Size() int64        { return es.node.Size }
func (es *entryStat) ModTime() time.Time { return es.creationTime }
func (es *entryStat) IsDir() bool        { return es.Mode().IsDir() } // [spec] Don't hardcode this.
func (es *entryStat) Sys() interface{}   { return es }

func (es *entryStat) Mode() fs.FileMode {
	// TODO: we should just stat the full node up front
	// mfs ents don't give us enough information about the type here
	switch mfs.NodeType(es.node.Type) {
	case mfs.TFile:
		return fs.FileMode(0)
	case mfs.TDir:
		return fs.ModeDir
	default:
		return fs.ModeIrregular
	}
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

/*
func (mi *mfsInterface) Stat(name string) (fs.FileInfo, error) {
	const op fserrors.Op = "mfs.Stat"
	if name == rootName {
		// return (*rootStat)(&mi.creationTime), nil
		// return mi.rootStat
		panic("NIY") // FIXME
	}

	// TODO: is there a direct way to do this?
	mfsNode, err := mfs.Lookup(mi.mroot, path.Join("/", name))
	if err != nil {
		return nil, fserrors.New(op, err) // TODO: context
	}
	ipldNode, err := mfsNode.GetNode()
	if err != nil {
		return nil, fserrors.New(op, err) // TODO: context
	}
	ufsNode, err := unixfs.ExtractFSNode(ipldNode)
	if err != nil {
		return nil, fserrors.New(op, err) // TODO: context
	}

	var typ fs.FileMode
	switch mfsNode.Type() {
	case mfs.TFile:
		typ = fs.FileMode(0)
	case mfs.TDir:
		typ = fs.ModeDir
	default:
		// Symlinks are not natively supported by MFS / the Files API
		// (But we'll support them)
		nodeType := ufsNode.Type()
		if nodeType == unixfs.TSymlink {
			typ = fs.ModeSymlink
			break
		}
		typ = fs.ModeIrregular
	}
	return &ipldStat{
		name:         path.Base(name),
		size:         int64(ufsNode.FileSize()),
		typ:          typ,
		creationTime: mi.creationTime,
	}, nil
}
*/

type ipldStat struct {
	name         string
	size         int64
	typ          fs.FileMode
	creationTime time.Time
}

func (is *ipldStat) Name() string       { return is.name }
func (is *ipldStat) Size() int64        { return is.size }
func (is *ipldStat) Mode() fs.FileMode  { return is.typ }
func (is *ipldStat) ModTime() time.Time { return is.creationTime }
func (is *ipldStat) IsDir() bool        { return is.Mode().IsDir() } // [spec] Don't hardcode this.
func (is *ipldStat) Sys() interface{}   { return is }

// TODO: quick ports below.
func (mi *mfsInterface) Make(name string) error {
	parentPath, childName := path.Split(name)
	parentNode, err := mfs.Lookup(mi.mroot, parentPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fserrors.New(fserrors.NotExist)
		}
		return fserrors.New(fserrors.Other)
	}
	parentDir, ok := parentNode.(*mfs.Directory)
	if !ok {
		return fserrors.New(fserrors.NotDir)
	}

	if _, err := parentDir.Child(childName); err == nil {
		return fserrors.New(fserrors.Exist)
	}

	dagFile := dag.NodeWithData(unixfs.FilePBData(nil, 0))
	dagFile.SetCidBuilder(parentDir.GetCidBuilder())
	if err := parentDir.AddChild(childName, dagFile); err != nil {
		return fserrors.New(fserrors.IO)
	}

	return nil
}

func (mi *mfsInterface) MakeDirectory(path string) error {
	if err := mfs.Mkdir(mi.mroot, path, mfs.MkdirOpts{Flush: true}); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fserrors.New(fserrors.Exist)
		}
		log.Println("hit4:", path)
		log.Println("dumping callstack")
		var i int
		for {
			pc, fn, line, ok := runtime.Caller(i)
			if !ok {
				break
			}
			log.Printf("/!\\ [sf{%d}] %s[%s:%d]\n", i, runtime.FuncForPC(pc).Name(), fn, line)
			i++
		}
		return fserrors.New(fserrors.Permission)
	}
	return nil
}

func (mi *mfsInterface) MakeLink(name, linkTarget string) error {
	parentPath, linkName := path.Split(name)
	parentNode, err := mfs.Lookup(mi.mroot, parentPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fserrors.New(fserrors.NotExist)
		}
		return fserrors.New(fserrors.Other)
	}

	parentDir, ok := parentNode.(*mfs.Directory)
	if !ok {
		return fserrors.New(fserrors.NotDir)
	}

	if _, err := parentDir.Child(linkName); err == nil {
		return fserrors.New(fserrors.Exist)
	}

	dagData, err := unixfs.SymlinkData(linkTarget)
	if err != nil {
		// TODO: SUS annotation
		return fserrors.New(fserrors.NotExist)
	}

	// TODO: same note as on keyfs; use raw node's for this if we can
	dagNode := dag.NodeWithData(dagData)
	dagNode.SetCidBuilder(parentDir.GetCidBuilder())

	if err := parentDir.AddChild(linkName, dagNode); err != nil {
		// SUSv7
		// "...or write permission is denied on the parent directory of the directory to be created"
		return fserrors.New(fserrors.NotExist)
	}
	return nil
}
