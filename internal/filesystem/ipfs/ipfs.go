package ipfs

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/hashicorp/golang-lru/arc/v2"
	coreiface "github.com/ipfs/boxo/coreiface"
	coreoptions "github.com/ipfs/boxo/coreiface/options"
	corepath "github.com/ipfs/boxo/coreiface/path"
	ipath "github.com/ipfs/boxo/path"
	"github.com/ipfs/boxo/path/resolver"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	ipld "github.com/ipfs/go-ipld-format"
)

type (
	ipfsRecord struct {
		ipld.Node
		*nodeInfo
	}
	ipfsNodeCache = arc.ARCCache[cid.Cid, ipfsRecord]
	ipfsDirCache  = arc.ARCCache[cid.Cid, []filesystem.StreamDirEntry]
	IPFS          struct {
		ctx         context.Context
		cancel      context.CancelFunc
		core        coreiface.CoreAPI
		resolver    resolver.Resolver
		nodeCache   *ipfsNodeCache
		dirCache    *ipfsDirCache
		info        nodeInfo
		nodeTimeout time.Duration
	}
	ipfsSettings struct {
		*IPFS
		defaultNodeCache,
		defaultDirCache bool
	}
	IPFSOption    func(*ipfsSettings) error
	ipfsDirectory struct {
		stream *entryStream
		info   *nodeInfo
		err    error
		cid    cid.Cid
	}
)

const IPFSID filesystem.ID = "IPFS"

func NewIPFS(core coreiface.CoreAPI, options ...IPFSOption) (*IPFS, error) {
	var (
		fsys = &IPFS{
			info: nodeInfo{
				name:    filesystem.Root,
				modTime: time.Now(),
				mode: fs.ModeDir |
					readAll | executeAll,
			},
			core:        core,
			nodeTimeout: 1 * time.Minute,
		}
		settings = ipfsSettings{
			IPFS:             fsys,
			defaultNodeCache: true,
			defaultDirCache:  true,
		}
	)
	if err := generic.ApplyOptions(&settings, options...); err != nil {
		return nil, err
	}
	if err := settings.fillInDefaults(); err != nil {
		fsys.cancel()
		return nil, err
	}
	fsys.resolver = newPathResolver(fsys.getNode)
	return fsys, nil
}

func (settings *ipfsSettings) fillInDefaults() error {
	if fsys := settings.IPFS; fsys.ctx == nil {
		fsys.ctx, fsys.cancel = context.WithCancel(context.Background())
	}
	const cacheCount = 64 // Arbitrary.
	if settings.defaultNodeCache {
		if err := settings.initNodeCache(cacheCount); err != nil {
			return err
		}
	}
	if settings.defaultDirCache {
		if err := settings.initDirectoryCache(cacheCount); err != nil {
			return err
		}
	}
	return nil
}

func (settings *ipfsSettings) initNodeCache(count int) error {
	nodeCache, err := arc.NewARC[cid.Cid, ipfsRecord](count)
	if err != nil {
		return err
	}
	settings.nodeCache = nodeCache
	return nil
}

func (settings *ipfsSettings) initDirectoryCache(count int) error {
	dirCache, err := arc.NewARC[cid.Cid, []filesystem.StreamDirEntry](count)
	if err != nil {
		return err
	}
	settings.dirCache = dirCache
	return nil
}

// WithNodeCacheCount sets the number of IPLD nodes the
// file system will hold in its cache.
// If <= 0, caching of nodes is disabled.
func WithNodeCacheCount(cacheCount int) IPFSOption {
	return func(ifs *ipfsSettings) error {
		ifs.defaultNodeCache = false
		if cacheCount <= 0 {
			return nil
		}
		return ifs.initNodeCache(cacheCount)
	}
}

// WithDirectoryCacheCount sets the number of directory
// entry-lists the file system will hold in its cache.
// If <= 0, caching of entries is disabled.
func WithDirectoryCacheCount(cacheCount int) IPFSOption {
	return func(ifs *ipfsSettings) error {
		ifs.defaultDirCache = false
		if cacheCount <= 0 {
			return nil
		}
		return ifs.initDirectoryCache(cacheCount)
	}
}

// WithNodeTimeout sets a timeout duration to use
// when communicating with the IPFS API/node.
// If <= 0, operations will not time out,
// and will remain pending until the file system is closed.
func WithNodeTimeout(duration time.Duration) IPFSOption {
	return func(ifs *ipfsSettings) error {
		ifs.nodeTimeout = duration
		return nil
	}
}

func (*IPFS) ID() filesystem.ID { return IPFSID }

func (fsys *IPFS) setContext(ctx context.Context) {
	fsys.ctx, fsys.cancel = context.WithCancel(ctx)
}

func (fsys *IPFS) setPermissions(permissions fs.FileMode) {
	fsys.info.mode = fsys.info.mode.Type() | permissions.Perm()
}

func (fsys *IPFS) Close() error {
	fsys.cancel()
	return nil
}

func (fsys *IPFS) Stat(name string) (fs.FileInfo, error) {
	const op = "stat"
	if name == filesystem.Root {
		return &fsys.info, nil
	}
	cid, err := fsys.toCID(op, name)
	if err != nil {
		return nil, err
	}
	info, err := fsys.getInfo(name, cid)
	if err != nil {
		return nil, fserrors.New(op, name, err, fserrors.IO)
	}
	return info, nil
}

func (fsys *IPFS) toCID(op, goPath string) (cid.Cid, error) {
	// NOTE: core.Resolve{Path,Node} is typically correct for this
	// but we're trying to avoid communicating with the node
	// as much as possible, and ResolveX is expensive when
	// we're getting hit frequently.
	// As such, we use the local information we have
	// and cache + make assumptions aggressively.
	var (
		names        = strings.Split(goPath, "/")
		rootCID, err = cid.Decode(names[0])
	)
	if err != nil {
		kind := cidErrKind(err)
		return cid.Cid{}, fserrors.New(op, goPath, err, kind)
	}
	if len(names) == 1 {
		return rootCID, nil
	}
	nodeCID, err := fsys.resolvePath(goPath)
	if err != nil {
		kind := resolveErrKind(err)
		return cid.Cid{}, fserrors.New(op, goPath, err, kind)
	}
	return nodeCID, nil
}

func (fsys *IPFS) getInfo(name string, cid cid.Cid) (*nodeInfo, error) {
	cache := fsys.nodeCache
	if cacheDisabled := cache == nil; cacheDisabled {
		return fsys.fetchInfo(name, cid)
	}
	record, _ := cache.Get(cid)
	if info := record.nodeInfo; info != nil {
		return info, nil
	}
	node := record.Node
	if node == nil {
		var err error
		if node, err = fsys.fetchNode(cid); err != nil {
			return nil, err
		}
		record.Node = node
	}
	var (
		rootInfo = fsys.info
		info     = nodeInfo{
			name:    name,
			modTime: rootInfo.modTime,
			mode:    rootInfo.mode.Perm(),
		}
	)
	if err := statNode(node, &info); err != nil {
		return nil, err
	}
	record.nodeInfo = &info
	cache.Add(cid, record)
	return &info, nil
}

func (fsys *IPFS) fetchInfo(name string, cid cid.Cid) (*nodeInfo, error) {
	node, err := fsys.getNode(cid)
	if err != nil {
		return nil, err
	}
	var (
		rootInfo = fsys.info
		info     = nodeInfo{
			name:    name,
			modTime: rootInfo.modTime,
			mode:    rootInfo.mode.Perm(),
		}
	)
	if err := statNode(node, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (fsys *IPFS) getNode(cid cid.Cid) (ipld.Node, error) {
	cache := fsys.nodeCache
	if cacheDisabled := cache == nil; cacheDisabled {
		return fsys.fetchNode(cid)
	}
	var (
		record, _ = cache.Get(cid)
		node      = record.Node
	)
	if node != nil {
		return node, nil
	}
	node, err := fsys.fetchNode(cid)
	if err != nil {
		return nil, err
	}
	record.Node = node
	cache.Add(cid, record)
	return node, nil
}

func (fsys *IPFS) fetchNode(cid cid.Cid) (ipld.Node, error) {
	ctx, cancel := fsys.nodeContext()
	defer cancel()
	return fsys.core.Dag().Get(ctx, cid)
}

func (fsys *IPFS) nodeContext() (context.Context, context.CancelFunc) {
	var (
		ctx     = fsys.ctx
		timeout = fsys.nodeTimeout
	)
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func (fsys *IPFS) resolvePath(goPath string) (cid.Cid, error) {
	var (
		ctx          = fsys.ctx
		resolver     = fsys.resolver
		iPath        = ipath.FromString(goPath)
		leaf, _, err = resolver.ResolveToLastNode(ctx, iPath)
	)
	return leaf, err
}

func (fsys *IPFS) Open(name string) (fs.File, error) {
	if name == filesystem.Root {
		return emptyRoot{info: &fsys.info}, nil
	}
	const op = "open"
	if err := validatePath(op, name); err != nil {
		return nil, err
	}
	cid, err := fsys.toCID(op, name)
	if err != nil {
		return nil, err
	}
	file, err := fsys.openCid(name, cid)
	if err != nil {
		return nil, fserrors.New(op, name, err, fserrors.IO)
	}
	return file, nil
}

func (fsys *IPFS) openCid(name string, cid cid.Cid) (fs.File, error) {
	info, err := fsys.getInfo(name, cid)
	if err != nil {
		return nil, err
	}
	switch typ := info.mode.Type(); typ {
	case fs.FileMode(0):
		return fsys.openFile(cid, info)
	case fs.ModeDir:
		return fsys.openDir(cid, info)
	default:
		return nil, fmt.Errorf(
			"%w got: \"%s\" want: regular file or directory",
			errUnexpectedType, fsTypeName(typ),
		)
	}
}

func (fsys *IPFS) openDir(cid cid.Cid, info *nodeInfo) (fs.File, error) {
	var (
		dirCtx, cancel = context.WithCancel(fsys.ctx)
		entries, err   = fsys.getEntries(dirCtx, cid, info)
	)
	if err != nil {
		cancel()
		return nil, err
	}
	return &ipfsDirectory{
		cid:  cid,
		info: info,
		stream: &entryStream{
			Context: dirCtx, CancelFunc: cancel,
			ch: entries,
		},
	}, nil
}

func (fsys *IPFS) getEntries(ctx context.Context, cid cid.Cid, info *nodeInfo) (<-chan filesystem.StreamDirEntry, error) {
	cache := fsys.dirCache
	if cacheDisabled := cache == nil; cacheDisabled {
		return fsys.fetchEntries(ctx, cid, info)
	}
	if entries, _ := cache.Get(cid); entries != nil {
		return generateEntryChan(ctx, entries), nil
	}
	return fsys.fetchAndCacheEntries(ctx, cid, info)
}

func (fsys *IPFS) fetchAndCacheEntries(ctx context.Context, cid cid.Cid, info *nodeInfo) (<-chan filesystem.StreamDirEntry, error) {
	fetchCtx, cancel := context.WithCancel(fsys.ctx)
	fetched, err := fsys.fetchEntries(fetchCtx, cid, info)
	if err != nil {
		cancel()
		return nil, err
	}
	var (
		relay       = newStreamChan(fetched)
		accumulator = make([]filesystem.StreamDirEntry, 0, cap(fetched))
	)
	go func() {
		defer cancel()
		sawError, snapshot := accumulateRelayClose(ctx, fetched, relay, accumulator)
		if sawError || fetchCtx.Err() != nil {
			return // Invalid|short results, don't cache.
		}
		snapshot = generic.CompactSlice(snapshot)
		fsys.dirCache.Add(cid, snapshot)
	}()
	return relay, nil
}

func (fsys *IPFS) fetchEntries(ctx context.Context, cid cid.Cid, info *nodeInfo) (<-chan filesystem.StreamDirEntry, error) {
	var (
		api          = fsys.core.Unixfs()
		path         = corepath.IpfsPath(cid)
		entries, err = api.Ls(ctx, path, coreoptions.Unixfs.ResolveChildren(true))
	)
	if err != nil {
		return nil, err
	}
	var (
		modTime     = info.modTime
		permissions = info.mode.Perm()
		converted   = newStreamChan(entries)
	)
	go func() {
		defer close(converted)
		for {
			select {
			case entry, ok := <-entries:
				if !ok {
					return
				}
				select {
				case converted <- &coreDirEntry{
					DirEntry:    entry,
					modTime:     modTime,
					permissions: permissions,
				}:
				case <-ctx.Done():
					drainThenSendErr(converted, ctx.Err())
					return
				}
			case <-ctx.Done():
				drainThenSendErr(converted, ctx.Err())
				return
			}
		}
	}()
	return converted, nil
}

func (fsys *IPFS) openFile(cid cid.Cid, info *nodeInfo) (fs.File, error) {
	ipldNode, err := fsys.getNode(cid)
	if err != nil {
		return nil, err
	}
	switch typedNode := ipldNode.(type) {
	case (*cbor.Node):
		return openCborFile(typedNode, info), nil
	default:
		var (
			ctx = fsys.ctx
			dag = fsys.core.Dag()
		)
		file, err := openUFSFile(ctx, dag, typedNode, info)
		if err != nil {
			// HACK: not exactly a proper error name.
			// But this only matters when debugging anyway.
			return nil, fserrors.New("openFile", cid.String(), err, ufsOpenErr(err))
		}
		return file, nil
	}
}

func (id *ipfsDirectory) Stat() (fs.FileInfo, error) { return id.info, nil }

func (id *ipfsDirectory) Read([]byte) (int, error) {
	const op = "read"
	return -1, fserrors.New(op, id.info.name, filesystem.ErrIsDir, fserrors.IsDir)
}

func (id *ipfsDirectory) StreamDir() <-chan filesystem.StreamDirEntry {
	const op = "streamdir"
	stream := id.stream
	if stream == nil {
		errs := make(chan filesystem.StreamDirEntry, 1)
		// TODO: We don't have an error kind
		// that translates into EBADF
		errs <- newErrorEntry(
			fserrors.New(op, id.info.name, fs.ErrClosed, fserrors.IO),
		)
		return errs
	}
	return stream.ch
}

func (id *ipfsDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	const op = "readdir"
	if err := id.err; err != nil {
		return nil, err
	}
	stream := id.stream
	if stream == nil {
		// TODO: We don't have an error kind
		// that translates into EBADF
		return nil, fserrors.New(op, id.info.name, fs.ErrClosed, fserrors.IO)
	}
	var (
		ctx       = stream.Context
		entryChan = stream.ch
	)
	entries, err := readEntries(ctx, entryChan, count)
	if err != nil {
		err = readdirErr(op, id.info.name, err)
		id.err = err
	}
	return entries, err
}

func (id *ipfsDirectory) Close() error {
	const op = "close"
	if stream := id.stream; stream != nil {
		stream.CancelFunc()
		id.stream = nil
		return nil
	}
	return fserrors.New(op, id.info.name, fs.ErrClosed, fserrors.InvalidItem)
}
