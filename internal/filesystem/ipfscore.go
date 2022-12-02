package filesystem

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	files "github.com/ipfs/go-ipfs-files"
	cbor "github.com/ipfs/go-ipld-cbor"
	ipld "github.com/ipfs/go-ipld-format" // TODO: migrate to new standard
	dag "github.com/ipfs/go-merkledag"
	"github.com/ipfs/go-unixfs"
	unixpb "github.com/ipfs/go-unixfs/pb"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	corepath "github.com/ipfs/interface-go-ipfs-core/path"
)

// TODO: move this to a test file
var _ StreamDirFile = (*coreDirectory)(nil)

const (
	// TODO: reconsider if we want these in `filesystem` or not.
	// We can probably just use more localy scoped consts
	// like ipfsRootPerms = x|y|z

	executeAll = ExecuteUser | ExecuteGroup | ExecuteOther
	writeAll   = WriteUser | WriteGroup | WriteOther
	readAll    = ReadUser | ReadGroup | ReadOther

	// These haven't even been used yet.

	allOther = ReadOther | WriteOther | ExecuteOther
	allGroup = ReadGroup | WriteGroup | ExecuteGroup
	allUser  = ReadUser | WriteUser | ExecuteUser
)

type (
	coreRootInfo struct {
		// TODO: track Atime;
		// m,c, and birth time can be the same as initTime.
		initTime time.Time
		mode     fs.FileMode
	}
	coreFS struct {
		rootInfo *coreRootInfo
		core     coreiface.CoreAPI
		systemID ID
	}
	coreDirectory struct {
		stat    fs.FileInfo
		entries <-chan coreiface.DirEntry
		context.Context
		context.CancelFunc
	}
	coreDirEntry struct {
		coreiface.DirEntry
		error
		initTime    time.Time
		permissions fs.FileMode
	}
	coreFileInfo struct {
		name     string
		size     int64
		mode     fs.FileMode
		initTime time.Time
	}
	coreFile struct {
		stat fs.FileInfo
		files.File
		cancel context.CancelFunc
	}
	ufsDirEntry struct {
		stat fs.FileInfo
		coreiface.DirEntry
	}
	cborFile struct {
		stat   fs.FileInfo
		node   *cbor.Node
		reader io.ReadSeeker
	}
)

const ipfsCoreTimeout = 10 * time.Second

func NewIPFS(core coreiface.CoreAPI, systemID ID) *coreFS {
	return &coreFS{
		rootInfo: &coreRootInfo{
			initTime: time.Now(),
		},
		core:     core,
		systemID: systemID,
	}
}

func (ci *coreFS) ID() ID { return ci.systemID }

func (ci *coreFS) Open(name string) (fs.File, error) {
	if name == rootName {
		return ci.rootInfo, nil
	}
	if !fs.ValidPath(name) {
		return nil,
			&fs.PathError{
				Op:   "open",
				Path: name,
				Err:  fserrors.New(fserrors.InvalidItem), // TODO: convert old-style errors.
			}
	}
	// TODO: OpenFile + read-only checking on flags
	corePath, err := goToIPFSCore(ci.systemID, name)
	if err != nil {
		// TODO: double check what error kind we should use for path errors.
		return nil, fserrors.New(fserrors.NotExist, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), ipfsCoreTimeout)
	defer cancel()
	ipldNode, err := ci.core.ResolveNode(ctx, corePath)
	if err != nil {
		// FIXME / NOTE: if we don't return not-found here
		// Windows will refuse to work with various binaries
		// when looking for their sidecar files
		// (E.g. file.exe.manifest, file.exe.config)
		return nil, fserrors.New(fserrors.NotExist, err)
	}
	return ci.openNode(name, corePath, ipldNode)
}

func (ci *coreFS) openNode(name string,
	corePath corepath.Path, ipldNode ipld.Node,
) (fs.File, error) {
	const (
		op                 fserrors.Op = "ipfscore.openNode"
		defaultPermissions             = readAll | executeAll
	)
	defaultMtime := ci.rootInfo.ModTime()
	stat, err := statNode(name, defaultMtime, defaultPermissions, ipldNode)
	if err != nil {
		return nil, fserrors.New(op,
			fserrors.Path(name),
			fserrors.IO, // TODO: [review] double check this Kind makes sense for this.
			err,
		)
	}
	var file fs.File
	switch stat.Mode().Type() { // TODO links, etc.
	case fs.FileMode(0):
		// TODO: pass stat
		file, err = openIPFSFile(name, ci.core, ipldNode)
	case fs.ModeDir:
		dirAPI := ci.core.Unixfs()
		// file, fErr = openIPFSDir(dirAPI, corePath, statFn, ci.creationTime)
		file, err = openIPFSDir(dirAPI, corePath, stat)
	default:
		// TODO: real error value+message
		err = fserrors.New("unsupported type")
	}
	if err != nil {
		return nil, fserrors.New(op,
			fserrors.Path(name),
			fserrors.IO, // TODO: [review] double check this Kind makes sense for this.
			err,
		)
	}
	return file, nil
}

func (ci *coreFS) Stat(name string) (fs.FileInfo, error) {
	const op fserrors.Op = "ipfscore.Stat"
	if name == rootName {
		return ci.rootInfo, nil
	}

	corePath, err := goToIPFSCore(ci.systemID, name)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), ipfsCoreTimeout)
	defer cancel()
	// TODO: we should store the resolved path on the object and join on to it.
	// Not just here, but everywhere resolvenode is called.
	ipldNode, err := ci.core.ResolveNode(ctx, corePath)
	if err != nil {
		return nil, err
	}

	// TODO: fetch from somewhere else
	modTime := ci.rootInfo.ModTime()
	permissions := ci.rootInfo.Mode().Perm()
	//

	stat, err := statNode(name, modTime, permissions, ipldNode)
	if err != nil {
		// TODO: upstream error value used to not comparable
		// is this still the case?
		if strings.Contains(err.Error(), "no link named") {
			return nil, fserrors.New(fserrors.NotExist, err)
		}
		return nil, fserrors.New(op,
			fserrors.Path(name),
			fserrors.IO, // TODO: [review] double check this Kind makes sense for this.
			err,
		)
	}
	return stat, nil
}

func (*coreRootInfo) Name() string          { return rootName }
func (*coreRootInfo) Size() int64           { return 0 }
func (cr *coreRootInfo) Mode() fs.FileMode  { return cr.mode }
func (cr *coreRootInfo) ModTime() time.Time { return cr.initTime }
func (cr *coreRootInfo) IsDir() bool        { return cr.Mode().IsDir() }
func (cr *coreRootInfo) Sys() any           { return cr }

func (cr *coreRootInfo) Stat() (fs.FileInfo, error) {
	return cr, nil
}

func (*coreRootInfo) Read([]byte) (int, error) {
	const op fserrors.Op = "root.Read"
	return -1, fserrors.New(op, fserrors.IsDir)
}

func (*coreRootInfo) Close() error { return nil }

func (cd *coreDirectory) Stat() (fs.FileInfo, error) { return cd.stat, nil }

func (*coreDirectory) Read([]byte) (int, error) {
	const op fserrors.Op = "coreDirectory.Read"
	return -1, fserrors.New(op, fserrors.IsDir)
}

// TODO: also implement StreamDirFile
// TODO: [676aa3d1-00ea-480b-9c1c-b9b4667cb0f7] - These functions overlap a lot.
// We could probably generalize this by just having a `transformFn`
// parameter to a generic version of this; `T => DirEntry; err`
func (cd *coreDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	const op fserrors.Op = "coreDirectory.ReadDir"
	var (
		ctx         = cd.Context
		coreEntries = cd.entries
	)
	if ctx == nil ||
		coreEntries == nil {
		return nil, fserrors.New(op, fserrors.IO) // TODO: error value for E-not-open?
	}

	const upperBound = 64
	var (
		entries   = make([]fs.DirEntry, 0, generic.Min(count, upperBound))
		returnAll = count <= 0
	)
	for {
		select {
		case coreEntry, ok := <-coreEntries:
			if !ok {
				if returnAll {
					return entries, nil
				}
				// FIXME: we only want to return EOF /after/ we hit it.
				// I.e. if we have 2 entries, and a count of 100,
				// we reuturn ([2]{a,b}, nil)
				// Only if we're called again, will we return (nil, EOF)
				/* Standard does this:
				n := len(d.entry) - d.offset
				if n == 0 && count > 0 {
				    return nil, io.EOF
				}
				if count > 0 && n > count {
				    n = count
				}
				list := make([]fs.DirEntry, n)
				for i := range list {
				    list[i] = &d.entry[d.offset+i]
				}
				d.offset += n
				return list, nil
				*/
				return entries, io.EOF
			}
			if err := coreEntry.Err; err != nil {
				return entries, err
			}
			entries = append(entries, &coreDirEntry{DirEntry: coreEntry})
			if !returnAll {
				if count--; count == 0 {
					return entries, nil
				}
			}
		case <-ctx.Done():
			return entries, ctx.Err()
		}
	}
}

func (cde *coreDirEntry) Name() string               { return cde.DirEntry.Name }
func (cde *coreDirEntry) IsDir() bool                { return cde.Type().IsDir() }
func (cde *coreDirEntry) Info() (fs.FileInfo, error) { return cde, nil }
func (cde *coreDirEntry) Size() int64                { return int64(cde.DirEntry.Size) }
func (cde *coreDirEntry) ModTime() time.Time         { return cde.initTime }
func (cde *coreDirEntry) Mode() fs.FileMode          { return cde.Type() | cde.permissions }
func (cde *coreDirEntry) Sys() any                   { return cde }
func (cde *coreDirEntry) Error() error               { return cde.error }
func (cde *coreDirEntry) Type() fs.FileMode {
	switch cde.DirEntry.Type {
	case coreiface.TDirectory:
		return fs.ModeDir
	case coreiface.TFile:
		return fs.FileMode(0)
	case coreiface.TSymlink:
		return fs.ModeSymlink
	default:
		return fs.ModeIrregular
	}
}

func (cd *coreDirectory) StreamDir(ctx context.Context) <-chan StreamDirEntry {
	var (
		coreEntries = cd.entries
		goEntries   = make(chan StreamDirEntry, cap(coreEntries))
	)
	go func() {
		defer close(goEntries)
		if coreEntries != nil {
			translateCoreEntries(ctx, coreEntries, goEntries)
			return
		}
		const op fserrors.Op = "coreDirectory.StreamDir"
		err := fserrors.New(op, fserrors.IO) // TODO: error value for E-not-open?
		select {
		case goEntries <- &coreDirEntry{error: err}:
		case <-ctx.Done():
		}
	}()
	return goEntries
}

func translateCoreEntries(ctx context.Context,
	coreEntries <-chan coreiface.DirEntry,
	goEntries chan<- StreamDirEntry,
) {
	for coreEntry := range coreEntries {
		var entry StreamDirEntry
		if err := coreEntry.Err; err != nil {
			entry = &coreDirEntry{error: err}
		} else {
			entry = &coreDirEntry{DirEntry: coreEntry}
		}
		select {
		case goEntries <- entry:
		case <-ctx.Done():
			return
		}
	}
}

func (cd *coreDirectory) Close() error {
	const op fserrors.Op = "coredir.Close"
	if cancel := cd.CancelFunc; cancel != nil {
		cancel()
		cd.Context = nil
		cd.CancelFunc = nil
		cd.entries = nil
		return nil
	}
	return fserrors.New(op,
		fserrors.InvalidItem, // TODO: Check POSIX expected values
		"directory was not open",
	)
}

func (cfi *coreFileInfo) Name() string       { return cfi.name }
func (cfi *coreFileInfo) Size() int64        { return cfi.size }
func (cfi *coreFileInfo) Mode() fs.FileMode  { return cfi.mode }
func (cfi *coreFileInfo) ModTime() time.Time { return cfi.initTime }
func (cfi *coreFileInfo) IsDir() bool        { return cfi.Mode().IsDir() }
func (cfi *coreFileInfo) Sys() any           { return cfi }

// TODO: [port]
// func (cio *coreFile) Write(_ []byte) (int, error)   { return 0, errReadOnly }
// func (cio *coreFile) Truncate(_ uint64) error       { return errReadOnly }
func (cio *coreFile) Close() error { defer cio.cancel(); return cio.File.Close() }

func (cio *coreFile) Stat() (fs.FileInfo, error) { return cio.stat, nil }

// func (cio *coreFile) Read(buff []byte) (int, error) { return cio.File.Read(buff) }
// func (cio *coreFile) Size() (int64, error)          { return cio.File.Size() }
func (cio *coreFile) Seek(offset int64, whence int) (int64, error) {
	return cio.File.Seek(offset, whence)
}

func (de *ufsDirEntry) Name() string { return de.DirEntry.Name }

func (de *ufsDirEntry) Info() (fs.FileInfo, error) { return de.stat, nil }

/*
func (de *ufsDirEntry) Info() (fs.FileInfo, error) {
	return &ipfsCoreStat{
		name:    de.DirEntry.Name,
		typ:     de.DirEntry.Type,
		size:    de.Size,
		modtime: de.stat.ModTime(),
	}, nil
}
*/

func (de *ufsDirEntry) Type() fs.FileMode {
	info, err := de.Info()
	if err != nil {
		return fs.ModeIrregular
	}
	return info.Mode() & fs.ModeType
}

func (de *ufsDirEntry) IsDir() bool { return de.Type()&fs.ModeDir != 0 }

// TODO: [port]
// func (cio *cborFile) Write(_ []byte) (int, error)   { return 0, errReadOnly }
// func (cio *cborFile) Truncate(_ uint64) error       { return errReadOnly }
func (cio *cborFile) Close() error { return nil }

func (cio *cborFile) Stat() (fs.FileInfo, error)    { return cio.stat, nil }
func (cio *cborFile) Read(buff []byte) (int, error) { return cio.reader.Read(buff) }

func (cio *cborFile) Size() (int64, error) {
	size, err := cio.node.Size()
	return int64(size), err
}

func (cio *cborFile) Seek(offset int64, whence int) (int64, error) {
	return cio.reader.Seek(offset, whence)
}

func openIPFSDir(unixfs coreiface.UnixfsAPI, corePath corepath.Path, stat fs.FileInfo) (fs.ReadDirFile, error) {
	ctx, cancel := context.WithCancel(context.Background())
	entries, err := unixfs.Ls(ctx, corePath)
	if err != nil {
		cancel()
		return nil, err
	}
	return &coreDirectory{
		stat:       stat,
		entries:    entries,
		Context:    ctx,
		CancelFunc: cancel,
	}, nil
}

func openIPFSFile(name string, core coreiface.CoreAPI, ipldNode ipld.Node,
) (fs.File, error) {
	switch typedNode := ipldNode.(type) {
	case (*cbor.Node):
		// TODO: we need to pipe through the formatting bool, or use a global const
		// (or just remove it altogether and always return the raw binary data)
		humanize := true
		return openCborNode(typedNode, humanize)
	default:
		return openUFSNode(name, core, typedNode)
	}
}

func openUFSNode(name string, core coreiface.CoreAPI, ipldNode ipld.Node,
) (fs.File, error) {
	typedNode, ok := ipldNode.(*dag.ProtoNode)
	if !ok {
		return nil, errors.New("TODO")
	}
	ufsNode, err := unixfs.ExtractFSNode(typedNode)
	if err != nil {
		return nil, err
	}

	var (
		ufsPath     = corepath.IpfsPath(ipldNode.Cid())
		ctx, cancel = context.WithCancel(context.Background())
	)
	apiNode, err := core.Unixfs().Get(ctx, ufsPath)
	if err != nil {
		cancel()
		return nil, err
	}

	fileNode, ok := apiNode.(files.File)
	if !ok {
		cancel()
		// TODO: make sure caller inspects our error value
		// We should return a unique standard error that they can .Is() against
		// So that proper error values can be used with the host
		// EISDIR, etc.
		return nil, fserrors.New(
			fserrors.IsDir,
			fmt.Errorf("unexpected node type: %T",
				apiNode,
			),
		)
	}
	// TODO: store/get permissions from root.
	const permissions = readAll | executeAll
	// TODO: store permissions on the type and do this in the method for Mode()
	mode := func() fs.FileMode {
		mode := permissions
		switch ufsNode.Type() {
		case unixpb.Data_Directory, unixpb.Data_HAMTShard:
			mode |= fs.ModeDir
		case unixpb.Data_Symlink:
			mode |= fs.ModeSymlink
		case unixpb.Data_File, unixpb.Data_Raw:
		// NOOP:  mode |= fs.FileMode(0)
		default:
			mode |= fs.ModeIrregular
		}
		return mode
	}()
	return &coreFile{
		stat: &coreFileInfo{
			name: name,
			size: int64(ufsNode.FileSize()),
			// mode:     unixfsTypeToGoType(ufsNode.Type()) | permissions,
			mode:     mode,
			initTime: time.Now(), // TODO: from root
		},
		File:   fileNode,
		cancel: cancel,
	}, nil
}

func openCborNode(cborNode *cbor.Node,
	formatData bool,
) (fs.File, error) {
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

	return &cborFile{node: cborNode, reader: br}, nil
}
