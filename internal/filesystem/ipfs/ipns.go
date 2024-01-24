package ipfs

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
	lru "github.com/hashicorp/golang-lru/v2"
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
	ipnsRootCache = lru.ARCCache[string, ipnsRecord]
	IPNS          struct {
		ctx         context.Context
		core        coreiface.CoreAPI
		resolver    resolver.Resolver
		ipfs        fs.FS
		cancel      context.CancelFunc
		rootCache   *ipnsRootCache
		info        nodeInfo
		nodeTimeout time.Duration
		expiry      time.Duration
	}
	ipnsSettings struct {
		*IPNS
		defaultRootCache bool
	}
	IPNSOption func(*ipnsSettings) error
	ipnsFile   struct {
		file      fs.File
		refreshFn func() error
	}
)

const IPNSID filesystem.ID = "IPNS"

func NewIPNS(core coreiface.CoreAPI, ipfs fs.FS, options ...IPNSOption) (*IPNS, error) {
	var (
		fsys = &IPNS{
			core: core,
			ipfs: ipfs,
			info: nodeInfo{
				name:    filesystem.Root,
				modTime: time.Now(),
				mode: fs.ModeDir |
					readAll | executeAll,
			},
			nodeTimeout: 1 * time.Minute,
		}
		settings = ipnsSettings{
			IPNS:             fsys,
			defaultRootCache: true,
		}
	)
	for _, setter := range options {
		if err := setter(&settings); err != nil {
			return nil, err
		}
	}
	if err := settings.fillInDefaults(); err != nil {
		fsys.cancel()
		return nil, err
	}
	return fsys, nil
}

func (settings *ipnsSettings) fillInDefaults() error {
	fsys := settings.IPNS
	if fsys.ctx == nil {
		fsys.ctx, fsys.cancel = context.WithCancel(context.Background())
	}
	if settings.defaultRootCache {
		const cacheCount = 256
		if err := settings.initRootCache(cacheCount); err != nil {
			return err
		}
	}
	if fsys.expiry == 0 {
		fsys.expiry = 1 * time.Minute
	}
	return nil
}

func (settings *ipnsSettings) initRootCache(cacheSize int) error {
	rootCache, err := lru.NewARC[string, ipnsRecord](cacheSize)
	if err != nil {
		return err
	}
	settings.IPNS.rootCache = rootCache
	return nil
}

// WithRootCache sets the number of root names to cache.
// Roots will be resolved and held in the cache until they expire
// or this count is exceeded.
// If <=0, caching of names is disabled.
func WithRootCache(cacheCount int) IPNSOption {
	return func(fsys *ipnsSettings) error {
		fsys.defaultRootCache = false
		if cacheCount <= 0 {
			return nil
		}
		return fsys.initRootCache(cacheCount)
	}
}

// CacheNodesFor sets how long a node is considered
// valid within the cache. After this time, the node
// will be refreshed during its next operation.
func CacheNodesFor(duration time.Duration) IPNSOption {
	return func(fsys *ipnsSettings) error {
		fsys.expiry = duration
		return nil
	}
}

func (*IPNS) ID() filesystem.ID { return IPNSID }

func (fsys *IPNS) setContext(ctx context.Context) {
	fsys.ctx, fsys.cancel = context.WithCancel(ctx)
}

func (fsys *IPNS) setPermissions(permissions fs.FileMode) {
	fsys.info.mode = fsys.info.mode.Type() | permissions.Perm()
}

func (fsys *IPNS) Close() error {
	fsys.cancel()
	if closer, ok := fsys.ipfs.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (fsys *IPNS) Stat(name string) (fs.FileInfo, error) {
	const op = "stat"
	if name == filesystem.Root {
		return &fsys.info, nil
	}
	cid, err := fsys.toCID(op, name)
	if err != nil {
		return nil, err
	}
	return fs.Stat(fsys.ipfs, cid.String())
}

func (fsys *IPNS) toCID(op, goPath string) (cid.Cid, error) {
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
			kind := resolveErrKind(err)
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
		kind := resolveErrKind(err)
		return cid.Cid{}, fserrors.New(op, goPath, err, kind)
	}
	return leafCid, nil
}

func (fsys *IPNS) fetchNode(cid cid.Cid) (ipld.Node, error) {
	ctx, cancel := fsys.nodeContext()
	defer cancel()
	return fsys.core.Dag().Get(ctx, cid)
}

func (fsys *IPNS) resolvePath(goPath string) (cid.Cid, error) {
	if ipfs, ok := fsys.ipfs.(*IPFS); ok {
		return ipfs.resolvePath(goPath)
	}
	resolver := fsys.resolver
	if resolver == nil {
		resolver = newPathResolver(fsys.fetchNode)
		fsys.resolver = resolver
	}
	var (
		ctx          = fsys.ctx
		iPath        = ipath.FromString(goPath)
		leaf, _, err = resolver.ResolveToLastNode(ctx, iPath)
	)
	return leaf, err
}

func (fsys *IPNS) nodeContext() (context.Context, context.CancelFunc) {
	var (
		ctx     = fsys.ctx
		timeout = fsys.nodeTimeout
	)
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func (fsys *IPNS) fetchCID(ctx context.Context, goPath string) (cid.Cid, error) {
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

func (fsys *IPNS) Open(name string) (fs.File, error) {
	if name == filesystem.Root {
		return emptyRoot{info: &fsys.info}, nil
	}
	const op = "open"
	if !fs.ValidPath(name) {
		return nil, fserrors.New(op, name, filesystem.ErrPath, fserrors.InvalidItem)
	}
	cid, err := fsys.toCID(op, name)
	if err != nil {
		return nil, err
	}
	ipfs := fsys.ipfs
	file, err := ipfs.Open(cid.String())
	if err != nil {
		return nil, fserrors.New(op, name, err, fserrors.IO)
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
	// TODO: these kinds of things should
	// use the new [errors.ErrUnsupported] value too.
	file := nf.file
	if directory, ok := file.(fs.ReadDirFile); ok {
		return directory.ReadDir(count)
	}
	var (
		name string
		err  error = filesystem.ErrIsDir
		kind       = fserrors.NotDir
	)
	if info, sErr := file.Stat(); sErr == nil {
		name = info.Name()
	} else {
		err = errors.Join(err, sErr)
		kind = fserrors.IO
	}
	return nil, fserrors.New("ReadDir", name, err, kind)
}
