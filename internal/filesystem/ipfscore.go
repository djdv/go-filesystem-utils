package filesystem

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	files "github.com/ipfs/go-ipfs-files"
	cbor "github.com/ipfs/go-ipld-cbor"
	ipld "github.com/ipfs/go-ipld-format" // TODO: migrate to new standard
	dag "github.com/ipfs/go-merkledag"
	"github.com/ipfs/go-unixfs"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	corepath "github.com/ipfs/interface-go-ipfs-core/path"
)

type (
	ipfsCoreAPI struct {
		core     coreiface.CoreAPI
		root     rootDirectory
		systemID ID
	}
	coreDirectory struct {
		stat    fs.FileInfo
		entries <-chan coreiface.DirEntry
		context.Context
		context.CancelFunc
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

func NewIPFS(core coreiface.CoreAPI, systemID ID) *ipfsCoreAPI {
	return &ipfsCoreAPI{
		root:     newRoot(s_IRXA, nil),
		core:     core,
		systemID: systemID,
	}
}

func (ci *ipfsCoreAPI) ID() ID { return ci.systemID }

func (ci *ipfsCoreAPI) Open(name string) (fs.File, error) {
	if name == rootName {
		return ci.OpenDir(name)
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

func (ci *ipfsCoreAPI) openNode(name string,
	corePath corepath.Path, ipldNode ipld.Node,
) (fs.File, error) {
	const (
		op                 fserrors.Op = "ipfscore.openNode"
		defaultPermissions             = s_IRXA
	)
	// stat, err := ci.stat(name, ipldNode)
	defaultMtime := ci.root.stat.ModTime()
	stat, err := statNode(name, defaultMtime, defaultPermissions, ipldNode)
	if err != nil {
		return nil, fserrors.New(op,
			fserrors.Path(name),
			fserrors.IO, // TODO: [review] double check this Kind makes sense for this.
			err,
		)
	}

	var (
		file fs.File
		fErr error
	)
	// TODO links, etc.
	switch stat.Mode().Type() {
	case fs.FileMode(0):
		file, fErr = openIPFSFile(name, ci.core, ipldNode)
	case fs.ModeDir:
		dirAPI := ci.core.Unixfs()
		// file, fErr = openIPFSDir(dirAPI, corePath, statFn, ci.creationTime)
		file, fErr = openIPFSDir(dirAPI, corePath, stat)
	default:
		// TODO: real error value+message
		fErr = fserrors.New("unsupported type")
	}
	if fErr != nil {
		return nil, fserrors.New(op,
			fserrors.Path(name),
			fserrors.IO, // TODO: [review] double check this Kind makes sense for this.
			fErr,
		)
	}
	return file, nil
}

func (ci *ipfsCoreAPI) OpenDir(name string) (fs.ReadDirFile, error) {
	const op fserrors.Op = "ipfscore.OpenDir"
	if name == rootName {
		return ci.root, nil
	}
	if !fs.ValidPath(name) {
		return nil, fserrors.New(op,
			fserrors.Path(name),
			fserrors.InvalidItem,
		)
	}

	corePath, err := goToIPFSCore(ci.systemID, name)
	if err != nil {
		// TODO: double check what error kind we should use for path errors.
		return nil, fserrors.New(fserrors.NotExist, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), ipfsCoreTimeout)
	defer cancel()
	ipldNode, err := ci.core.ResolveNode(ctx, corePath)
	if err != nil {
		return nil, fserrors.New(op,
			fserrors.Permission, // TODO: check POSIX spec; this should probably be IO
			fserrors.Path(name),
			err,
		)
	}
	const (
		defaultPermissions = s_IRXA
	)
	// stat, err := ci.stat(name, ipldNode)
	defaultMtime := ci.root.stat.ModTime()
	stat, err := statNode(name, defaultMtime, defaultPermissions, ipldNode)
	if err != nil {
		return nil, fserrors.New(op,
			fserrors.Path(name),
			fserrors.IO, // TODO: [review] double check this Kind makes sense for this.
			err,
		)
	}

	directory, err := openIPFSDir(ci.core.Unixfs(), corePath, stat)
	if err != nil {
		return nil, fserrors.New(op,
			fserrors.Path(name),
			fserrors.IO, // TODO: [review] double check this Kind makes sense for this.
			err,
		)
	}
	return directory, nil
}

/*
func (ci *ipfsCoreAPI) Stat(name string) (fs.FileInfo, error) {
	const op fserrors.Op = "ipfscore.Stat"
	if name == rootName {
		return ci.root.stat, nil
	}
	var (
		corePath    = goToIPFSCore(ci.systemID, name)
		ctx, cancel = context.WithTimeout(context.Background(), ipfsCoreTimeout)
		err         error
		ipldNode    ipld.Node
	)
	defer cancel()
	if ipldNode, err = ci.core.ResolveNode(ctx, corePath); err != nil {
		return nil, err
	}
	stat, err := ci.stat(name, ipldNode)
	if err != nil {
		// TODO: if the cmds lib doesn't have a typed error we can use with .Is
		// one should be added for this. Checking messages like this is not stable.
		cmdsErr := new(cmds.Error)
		if errors.As(err, &cmdsErr) &&
			strings.Contains(cmdsErr.Message, "no link named") {
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
*/

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
	const (
		upperBound             = 64
		op         fserrors.Op = "coreDirectory.ReadDir"
	)
	var (
		ctx         = cd.Context
		coreEntries = cd.entries
		entries     = make([]fs.DirEntry, 0, generic.Min(count, upperBound))
		returnAll   = count <= 0
	)
	if ctx == nil {
		return nil, fserrors.New(op, fserrors.IO) // TODO: error value for E-not-open?
	}
	if coreEntries == nil {
		return nil, fserrors.New(op, fserrors.IO) // TODO: error value for E-not-open?
	}
	for {
		select {
		case <-ctx.Done():
			return entries, ctx.Err()
		case coreEntry, ok := <-coreEntries:
			if !ok {
				var err error
				if !returnAll {
					err = io.EOF
				}
				return entries, err
			}
			if err := coreEntry.Err; err != nil {
				return entries, err
			}
			// TODO: this typing is kind of weird. It should at least have an xEntry alias.
			entry := staticStat{
				name: coreEntry.Name,
				size: int64(coreEntry.Size),
				mode: coreTypeToGoType(coreEntry.Type) |
					s_IRXA, // TODO: from root.
				modTime: time.Now(), // TODO: from root.
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
	return &coreFile{
		stat: staticStat{
			name: name,
			size: int64(ufsNode.FileSize()),
			mode: unixfsTypeToGoType(ufsNode.Type()) |
				s_IRXA, // TODO: from root
			modTime: time.Now(), // TODO: from root
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

func coreTypeToGoType(typ coreiface.FileType) fs.FileMode {
	switch typ {
	case coreiface.TDirectory:
		return fs.ModeDir
	case coreiface.TFile:
		return fs.FileMode(0)
	case coreiface.TSymlink:
		return fs.ModeSymlink
	default:
		panic(fmt.Errorf(
			"mode: stat contains unexpected type: %v",
			typ,
		))
	}
}
