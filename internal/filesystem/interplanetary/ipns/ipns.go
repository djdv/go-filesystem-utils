package ipns

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	intp "github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/internal"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/ipfs"
	"github.com/hashicorp/golang-lru/arc/v2"
	coreiface "github.com/ipfs/boxo/coreiface"
	corepath "github.com/ipfs/boxo/coreiface/path"
	ipath "github.com/ipfs/boxo/path"
	"github.com/ipfs/boxo/path/resolver"
	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format"
)

type (
	ipnsRecord struct {
		*cid.Cid
		*time.Time
	}
	ipnsRootCache = arc.ARCCache[string, ipnsRecord]
	// FS implements [fs.FS] and [filesystem] extensions.
	FS struct {
		ctx        context.Context
		cancel     context.CancelFunc
		core       coreiface.CoreAPI
		resolver   resolver.Resolver
		ipfs       fs.FS
		rootCache  *ipnsRootCache
		info       intp.NodeInfo
		apiTimeout time.Duration
		expiry     time.Duration
		linkLimit  uint
	}
	ipnsFile struct {
		file      fs.File
		refreshFn func() error
	}
)

// ID defines the identifier of this system.
const ID filesystem.ID = "IPNS"

// New constructs an [FS] using the defaults listed in the pkg constants.
// A list of [Option] values can be provided to change these defaults as desired.
func New(core coreiface.CoreAPI, options ...Option) (*FS, error) {
	var (
		fsys = &FS{
			core: core,
			info: intp.NodeInfo{
				Name_:    filesystem.Root,
				ModTime_: time.Now(),
				Mode_:    fs.ModeDir | DefaultPermissions,
			},
			apiTimeout: DefaultAPITimeout,
			linkLimit:  DefaultLinkLimit,
			expiry:     DefaultCacheExpiry,
		}
		settings = settings{
			FS:               fsys,
			defaultRootCache: true,
		}
	)
	for _, setter := range options {
		if err := setter(&settings); err != nil {
			return nil, err
		}
	}
	if err := settings.fillInDefaults(core); err != nil {
		fsys.cancel()
		return nil, err
	}
	return fsys, nil
}

func (settings *settings) fillInDefaults(core coreiface.CoreAPI) error {
	fsys := settings.FS
	if fsys.ctx == nil {
		fsys.ctx, fsys.cancel = context.WithCancel(context.Background())
	}
	if settings.defaultRootCache {
		const cacheCount = 256
		if err := settings.initRootCache(cacheCount); err != nil {
			return err
		}
	}
	if fsys.ipfs == nil {
		ipfs, err := ipfs.New(core, settings.ipfsOptions...)
		if err != nil {
			return err
		}
		fsys.ipfs = ipfs
	}
	return nil
}

func (settings *settings) initRootCache(cacheSize int) error {
	rootCache, err := arc.NewARC[string, ipnsRecord](cacheSize)
	if err != nil {
		return err
	}
	settings.FS.rootCache = rootCache
	return nil
}

func (*FS) ID() filesystem.ID { return ID }

func (fsys *FS) Close() error {
	fsys.cancel()
	if closer, ok := fsys.ipfs.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (fsys *FS) Lstat(name string) (fs.FileInfo, error) {
	const op = "lstat"
	return fsys.stat(op, name, filesystem.Lstat)
}

func (fsys *FS) Stat(name string) (fs.FileInfo, error) {
	const op = "stat"
	return fsys.stat(op, name, fs.Stat)
}

func (fsys *FS) stat(op, name string, statFn intp.StatFunc) (fs.FileInfo, error) {
	if name == filesystem.Root {
		return &fsys.info, nil
	}
	cid, err := fsys.toCID(op, name)
	if err != nil {
		return nil, err
	}
	return statFn(fsys.ipfs, cid.String())
}

func (fsys *FS) toCID(op, goPath string) (cid.Cid, error) {
	var (
		names     = strings.Split(goPath, "/")
		root      = names[0]
		record, _ = fsys.rootCache.Peek(root)
		rootCID   cid.Cid
	)
	if cached, added := record.Cid, record.Time; cached != nil &&
		added != nil &&
		time.Since(*added) < fsys.expiry {
		rootCID = *cached
	} else {
		var (
			err         error
			ctx, cancel = fsys.nodeContext()
		)
		defer cancel()
		if rootCID, err = fsys.fetchCID(ctx, goPath); err != nil {
			kind := intp.ResolveErrKind(err)
			return cid.Cid{}, fserrors.New(op, goPath, err, kind)
		}
		record.Cid = &rootCID
		now := time.Now()
		record.Time = &now
		fsys.rootCache.Add(root, record)
	}
	if len(names) == 1 {
		return rootCID, nil
	}
	var (
		components = append(
			[]string{rootCID.String()},
			names[1:]...,
		)
		ipfsPath     = path.Join(components...)
		leafCid, err = fsys.resolvePath(ipfsPath)
	)
	if err != nil {
		kind := intp.ResolveErrKind(err)
		return cid.Cid{}, fserrors.New(op, goPath, err, kind)
	}
	return leafCid, nil
}

func (fsys *FS) fetchNode(cid cid.Cid) (ipld.Node, error) {
	ctx, cancel := fsys.nodeContext()
	defer cancel()
	return fsys.core.Dag().Get(ctx, cid)
}

func (fsys *FS) resolvePath(goPath string) (cid.Cid, error) {
	if ipfs, ok := fsys.ipfs.(*ipfs.FS); ok {
		return ipfs.ResolvePath(goPath)
	}
	resolver := fsys.resolver
	if resolver == nil {
		resolver = intp.NewPathResolver(fsys.fetchNode)
		fsys.resolver = resolver
	}
	var (
		ctx          = fsys.ctx
		iPath        = ipath.FromString(goPath)
		leaf, _, err = resolver.ResolveToLastNode(ctx, iPath)
	)
	return leaf, err
}

func (fsys *FS) nodeContext() (context.Context, context.CancelFunc) {
	ctx := fsys.ctx
	if timeout := fsys.apiTimeout; timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return context.WithCancel(ctx)
}

func (fsys *FS) fetchCID(ctx context.Context, goPath string) (cid.Cid, error) {
	var (
		corePath      = corepath.New(path.Join("/ipns", goPath))
		core          = fsys.core
		resolved, err = core.ResolvePath(ctx, corePath)
	)
	if err != nil {
		return cid.Cid{}, err
	}
	return resolved.Cid(), nil
}

func (fsys *FS) ReadLink(name string) (string, error) {
	const op = "readlink"
	if name == filesystem.Root {
		const kind = fserrors.InvalidItem
		return "", fserrors.New(op, name, intp.ErrRootLink, kind)
	}
	cid, err := fsys.toCID(op, name)
	if err != nil {
		return "", err
	}
	return filesystem.Readlink(fsys.ipfs, cid.String())
}

func (fsys *FS) resolveCIDSymlink(op, name string, cid cid.Cid) (string, error) {
	var (
		ufs         = fsys.core.Unixfs()
		ctx, cancel = fsys.nodeContext()
	)
	defer cancel()
	const allowedPrefix = "/ipns/"
	return intp.GetUnixFSLink(ctx, op, name, ufs, cid, allowedPrefix)
}

func (fsys *FS) Open(name string) (fs.File, error) {
	const depth = 0
	return fsys.open(name, depth)
}

func (fsys *FS) open(name string, depth uint) (fs.File, error) {
	if name == filesystem.Root {
		return intp.EmptyRoot{Info: &fsys.info}, nil
	}
	const op = "open"
	if err := intp.ValidatePath(op, name); err != nil {
		return nil, err
	}
	cid, err := fsys.toCID(op, name)
	if err != nil {
		return nil, err
	}
	ipfs := fsys.ipfs
	info, err := filesystem.Lstat(ipfs, cid.String())
	if err != nil {
		return nil, err
	}
	if info.Mode().Type() == fs.ModeSymlink {
		if depth++; depth >= fsys.linkLimit {
			return nil, intp.LinkLimitError(op, name, fsys.linkLimit)
		}
		target, err := fsys.resolveCIDSymlink(op, name, cid)
		if err != nil {
			return nil, err
		}
		return fsys.open(target, depth)
	}
	file, err := ipfs.Open(cid.String())
	if err != nil {
		return nil, err
	}
	nFile := ipnsFile{
		file: file,
	}
	nFile.refreshFn = func() error {
		fetchedCID, err := fsys.toCID(op, name)
		if err != nil {
			return err
		}
		if fetchedCID == cid {
			return nil
		}
		newFile, err := ipfs.Open(cid.String())
		if err != nil {
			return err
		}
		if err := seekToSame(file, newFile); err != nil {
			err = fserrors.New(op, name, err, fserrors.IO)
			if cErr := newFile.Close(); cErr != nil {
				return errors.Join(err, cErr)
			}
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		cid = fetchedCID
		nFile.file = newFile
		return nil
	}
	return &nFile, nil
}

func seekToSame(original, newFile fs.File) error {
	var (
		seeker, isSeeker       = original.(io.Seeker)
		newSeeker, newIsSeeker = newFile.(io.Seeker)
		matched                = isSeeker == newIsSeeker
	)
	if !matched {
		return formatSeekerErr(original, newFile, isSeeker, newIsSeeker)
	}
	if !isSeeker {
		return nil
	}
	cursor, err := seeker.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	_, err = newSeeker.Seek(cursor, io.SeekStart)
	return err
}

func formatSeekerErr(
	origFile, newFile any,
	origImpl, newImpl bool,
) error {
	// Format:
	// cannot update offset; old file `${%T}`
	// $[does not ]implement$[s] `io.Seeker` while new
	// file `${%T}` does$[ not]
	const (
		prefix        = "cannot update offset; "
		file          = "file "
		headOld       = "old " + file
		headNew       = "new " + file
		bodyOk        = " implements "
		tailOk        = " does"
		tailNotOk     = tailOk + " not"
		bodyNotOk     = tailNotOk + " implement "
		interfaceName = "`io.Seeker`"
		joiner        = " while "
	)
	var (
		b        strings.Builder
		origType = fmt.Sprintf("`%T`", origFile)
		newType  = fmt.Sprintf("`%T`", newFile)
		size     = len(prefix) +
			len(headOld) + len(headNew) +
			len(origType) + len(newType) +
			len(interfaceName) + len(joiner)
	)
	if origImpl {
		size += len(bodyOk)
	} else {
		size += len(bodyNotOk)
	}
	if newImpl {
		size += len(tailOk)
	} else {
		size += len(tailNotOk)
	}
	b.Grow(size)
	b.WriteString(prefix)

	b.WriteString(headOld)
	b.WriteString(origType)
	if origImpl {
		b.WriteString(bodyOk)
	} else {
		b.WriteString(bodyNotOk)
	}
	b.WriteString(interfaceName)

	b.WriteString(joiner)

	b.WriteString(headNew)
	b.WriteString(newType)
	if newImpl {
		b.WriteString(tailOk)
	} else {
		b.WriteString(tailNotOk)
	}
	return errors.New(b.String())
}

func (nf *ipnsFile) Close() error { return nf.file.Close() }
func (nf *ipnsFile) Stat() (fs.FileInfo, error) {
	if err := nf.refreshFn(); err != nil {
		return nil, err
	}
	return nf.file.Stat()
}

func (nf *ipnsFile) Seek(offset int64, whence int) (int64, error) {
	if err := nf.refreshFn(); err != nil {
		return 0, err
	}
	if seeker, ok := nf.file.(io.Seeker); ok {
		return seeker.Seek(offset, whence)
	}
	return 0, errors.ErrUnsupported
}

func (nf *ipnsFile) Read(b []byte) (int, error) {
	if err := nf.refreshFn(); err != nil {
		return 0, err
	}
	return nf.file.Read(b)
}

func (nf *ipnsFile) ReadDir(count int) ([]fs.DirEntry, error) {
	if err := nf.refreshFn(); err != nil {
		return nil, err
	}
	file := nf.file
	if directory, ok := file.(fs.ReadDirFile); ok {
		return directory.ReadDir(count)
	}
	var (
		name string
		err  = errors.ErrUnsupported
		kind fserrors.Kind
	)
	if info, sErr := file.Stat(); sErr == nil {
		name = info.Name()
		kind = fserrors.InvalidOperation
	} else {
		err = errors.Join(err, sErr)
		kind = fserrors.IO
	}
	return nil, fserrors.New("ReadDir", name, err, kind)
}

func conflictErr(name, conflict string) error {
	return fmt.Errorf(
		`cannot combine option "%s" with "%s"`,
		name, conflict,
	)
}
