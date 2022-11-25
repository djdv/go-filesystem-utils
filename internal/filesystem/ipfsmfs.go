package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"reflect"
	"time"
	"unsafe"

	// TODO: migrate to new standard
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"
	dag "github.com/ipfs/go-merkledag"
	"github.com/ipfs/go-mfs"
	"github.com/ipfs/go-unixfs"
	uio "github.com/ipfs/go-unixfs/io"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

// TODO: move this to a test file
var _ IDFS = (*IPFSMFS)(nil)

// TODO: _ StreamDirFile = (*mfsDirectory)(nil)

type (
	IPFSMFS struct {
		creationTime time.Time
		mroot        *mfs.Root
	}
	mfsDirectory struct {
		ctx     context.Context
		cancel  context.CancelFunc
		listing <-chan unixfs.LinkResult
		mfsDir  *mfs.Directory
		stat    *staticStat
	}
	mfsFile struct {
		descriptor mfs.FileDescriptor
		stat       *mfsStat
	}
	mfsStat struct {
		sizeFn func() (int64, error)
		staticStat
	}
)

func NewMFS(mroot *mfs.Root) fs.FS {
	return &IPFSMFS{
		creationTime: time.Now(), // TODO: take in metadata from options.
		mroot:        mroot,
	}
}

// TODO: we should probably not export this, and instead use options on the NewInterface constructor
// E.g. `WithMFSRoot(mroot)`, `WithCID(cid)`, or none which uses an empty directory by default.
// (unixfs.EmptyDirNode().Cid())
func CidToMFSRoot(ctx context.Context, root cid.Cid, core coreiface.CoreAPI, publish mfs.PubFunc) (*mfs.Root, error) {
	if !root.Defined() {
		return nil, errors.New("root CID was not defined")
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second) // TODO: const timeout
	defer cancel()
	ipldNode, err := core.Dag().Get(callCtx, root)
	if err != nil {
		return nil, err
	}
	pbNode, ok := ipldNode.(*dag.ProtoNode)
	if !ok {
		return nil, fmt.Errorf("%q has incompatible type %T", root.String(), ipldNode)
	}
	return mfs.NewRoot(ctx, core.Dag(), pbNode, publish)
}

func (mi *IPFSMFS) ID() ID       { return MFS }
func (mi *IPFSMFS) Close() error { return mi.mroot.Close() }

/* TODO
func (mi *IPFSMFS) Rename(oldName, newName string) error {
	const op fserrors.Op = "mfs.Rename"
	if err := mfs.Mv(mi.mroot, oldName, newName); err != nil {
		return fserrors.New(op,
			fserrors.Path(oldName+" -> "+newName), // TODO support multiple paths in New, we can switch off op suffix or a real type in Error() fmt
			fserrors.IO,
		)
	}
	return nil
}
*/

func (mi *IPFSMFS) Open(name string) (fs.File, error) {
	if name == rootName {
		return mi.openRoot()
	}
	mfsNode, err := mi.openNode(name)
	if err != nil {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  err,
		}
	}
	var file fs.File
	if mfsNode.Type() == mfs.TFile {
		file, err = mi.openFileNode(name, mfsNode, os.O_RDONLY)
	} else {
		file, err = mi.openDirNode(name, mfsNode)
	}
	if err != nil {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  err,
		}
	}
	return file, nil
}

func (mi *IPFSMFS) openNode(name string) (mfs.FSNode, error) {
	const op fserrors.Op = "mfs.openNode"
	if !fs.ValidPath(name) {
		return nil, fserrors.New(op, fserrors.InvalidItem) // TODO: convert old-style errors.
	}

	mfsNode, err := mfs.Lookup(mi.mroot, path.Join("/", name))
	if err != nil {
		var errKind fserrors.Kind
		if errors.Is(err, os.ErrNotExist) {
			errKind = fserrors.NotExist
		} else {
			errKind = fserrors.Permission // TODO: EIO? Something else?
		}
		return nil, fserrors.New(op,
			fserrors.Path(name),
			errKind,
		)
	}
	return mfsNode, nil
}

func (mi *IPFSMFS) openFileNode(name string, mfsNode mfs.FSNode, flag int) (fs.File, error) {
	mfsFileIntf, ok := mfsNode.(*mfs.File)
	if !ok {
		// TODO: error value.
		// What if this node is a link or something else.
		return nil, fserrors.New(fserrors.IsDir) // TODO: convert old-style errors.
	}
	var mfsFlags mfs.Flags
	switch {
	case flag&os.O_WRONLY != 0:
		mfsFlags.Write = true
		mfsFlags.Sync = true
	case flag&os.O_RDWR != 0:
		mfsFlags.Read = true
		mfsFlags.Write = true
		mfsFlags.Sync = true
	default:
		mfsFlags.Read = true
	}
	descriptor, err := mfsFileIntf.Open(mfsFlags)
	if err != nil {
		return nil, fserrors.New(fserrors.Permission)
	}
	const permissions = readAll | writeAll | executeAll // TODO: set/get perms from elsewhere.
	return &mfsFile{
		descriptor: descriptor,
		stat: &mfsStat{ // TODO: retrieve metadata from node if present; timestamps from constructor otherwise.
			sizeFn: mfsFileIntf.Size,
			staticStat: staticStat{
				name:    path.Base(name),
				mode:    permissions,
				modTime: time.Now(),
			},
		},
	}, nil
}

func (mi *IPFSMFS) OpenFile(name string, flag int, perm fs.FileMode) (fs.File, error) {
	const op fserrors.Op = "mfs.OpenFile"
	if name == rootName {
		return nil, &fs.PathError{
			Op:   "open", // TODO: is this right for extensions?
			Path: name,
			Err:  fserrors.New(op, fserrors.IsDir), // TODO: convert old-style errors.
		}
	}
	mfsNode, err := mi.openNode(name)
	if err != nil {
		return nil, &fs.PathError{
			Op:   "open", // TODO: is this right for extensions?
			Path: name,
			Err:  err,
		}
	}
	file, err := mi.openFileNode(name, mfsNode, flag)
	if err != nil {
		return nil, &fs.PathError{
			Op:   "open", // TODO: is this right for extensions?
			Path: name,
			Err:  fserrors.New(op, fserrors.IsDir), // TODO: convert old-style errors.
		}
	}
	return file, nil
}

func (mi *IPFSMFS) openRoot() (fs.File, error) {
	mfsNode, err := mfs.Lookup(mi.mroot, "/")
	if err != nil {
		return nil, err
	}
	return mi.openDirNode("/", mfsNode)
}

func (mi *IPFSMFS) openDirNode(name string, mfsNode mfs.FSNode) (fs.ReadDirFile, error) {
	const op fserrors.Op = "mfs.openDirNode"
	mfsDir, isDir := mfsNode.(*mfs.Directory)
	if !isDir {
		return nil, fserrors.New(fserrors.NotDir) // TODO: convert old-style errors.
	}
	ctx, cancel := context.WithCancel(context.Background())
	const permissions = readAll | writeAll | executeAll // TODO: set/get perms from elsewhere.
	return &mfsDirectory{
		ctx: ctx, cancel: cancel,
		listing: _hackListAsync(ctx, mfsDir),
		mfsDir:  mfsDir,
		stat: &staticStat{ // TODO: retrieve metadata from node if present; timestamps from constructor otherwise.
			name:    path.Base(name),
			mode:    fs.ModeDir | permissions,
			modTime: time.Now(),
		},
	}, nil
}

func _hackListAsync(ctx context.Context, mfsDir *mfs.Directory) <-chan unixfs.LinkResult {
	// TODO: we need to fork the mfs lib to expose [mfsDir.ListAsync()] formally.
	// Our old hack was to reconstruct a uio.Directory from mfsdir.GetNode + dagservice.
	// But that is extremely roundabout when that interface is already in our stack.
	var ( // HACK: Politely ask the runtime to let us read private data.
		field = reflect.ValueOf(mfsDir).Elem().
			FieldByName("unixfsDir")
		srcAddr = unsafe.Pointer(field.UnsafeAddr())
		hax     = reflect.NewAt(field.Type(), srcAddr).Elem()
	)
	return hax.Interface().(uio.Directory).EnumLinksAsync(ctx)
}

// func (md *mfsDirectory) Stat() (fs.FileInfo, error) { return &md.stat, nil }
func (md *mfsDirectory) Stat() (fs.FileInfo, error) { return md.stat, nil }

func (*mfsDirectory) Read([]byte) (int, error) {
	const op fserrors.Op = "mfsDirectory.Read"
	return -1, fserrors.New(op, fserrors.IsDir)
}

func (md *mfsDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	const op fserrors.Op = "mfsDirectory.ReadDir"
	var (
		ctx     = md.ctx
		listing = md.listing
		parent  = md.mfsDir
	)
	if ctx == nil ||
		listing == nil ||
		parent == nil {
		return nil, fserrors.New(op, fserrors.IO) // TODO: error value for E-not-open?
	}

	const upperBound = 64
	var (
		entries   = make([]fs.DirEntry, 0, generic.Min(count, upperBound))
		returnAll = count <= 0
	)
	for {
		select {
		case <-ctx.Done():
			return entries, ctx.Err()
		case link, ok := <-listing:
			if !ok {
				var err error
				if !returnAll {
					err = io.EOF
				}
				return entries, err
			}
			if err := link.Err; err != nil {
				return entries, err
			}
			entry, err := translateUFSLinkEntry(parent, link.Link)
			if err != nil {
				return entries, err
			}
			entries = append(entries, entry)
			if !returnAll {
				if count--; count == 0 {
					return entries, nil
				}
			}
		}
	}
}

func translateUFSLinkEntry(parent *mfs.Directory, link *ipld.Link) (fs.DirEntry, error) {
	name := link.Name
	child, err := parent.Child(name)
	if err != nil {
		return nil, err
	}
	/*
		TODO: Symlinks are not currently supported by go-mfs / the IPFS Files API.
		But we used to support them in our mount logic and encountered no problems.
		We'll have to port over the code for special file types.
		ipldNode, _ := mfsNode.GetNode()
		ufsNode, _ := unixfs.ExtractFSNode(ipldNode)
		nodeType := ufsNode.Type()
		if nodeType == unixfs.TSymlink {
			typ = fs.ModeSymlink
		}
	*/
	const permissions = readAll | writeAll | executeAll // TODO: perms from caller or node if present.
	var (
		mode fs.FileMode
		size int64
	)
	if child.Type() == mfs.TDir {
		mode |= fs.ModeDir
	} else {
		if file, ok := child.(*mfs.File); ok {
			if size, err = file.Size(); err != nil {
				return nil, err
			}
		}
	}
	return &staticStat{
		name:    name,
		size:    size,
		mode:    mode | permissions,
		modTime: time.Now(), // TODO: time from somewhere else
	}, nil
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

func (ms *mfsStat) Size() int64 { s, _ := ms.sizeFn(); return s }

func (mio *mfsFile) Read(buff []byte) (int, error) {
	return mio.descriptor.Read(buff)
}
func (mio *mfsFile) Write(buff []byte) (int, error) { return mio.descriptor.Write(buff) }
func (mio *mfsFile) Truncate(size uint64) error     { return mio.descriptor.Truncate(int64(size)) }
func (mio *mfsFile) Close() error                   { return mio.descriptor.Close() }
func (mio *mfsFile) Seek(offset int64, whence int) (int64, error) {
	return mio.descriptor.Seek(offset, whence)
}

func (mio *mfsFile) Stat() (fs.FileInfo, error) { return mio.stat, nil }

// TODO: quick ports below.
func (mi *IPFSMFS) Make(name string) error {
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

func (mi *IPFSMFS) MakeDirectory(path string) error {
	if err := mfs.Mkdir(mi.mroot, path, mfs.MkdirOpts{Flush: true}); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fserrors.New(fserrors.Exist)
		}
		return fserrors.New(fserrors.Permission)
	}
	return nil
}

func (mi *IPFSMFS) MakeLink(name, linkTarget string) error {
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
