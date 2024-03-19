package ipfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	intp "github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/internal"
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
	"github.com/multiformats/go-multibase"
)

type (
	ipfsRecord struct {
		ipld.Node
		*intp.NodeInfo
	}
	ipfsNodeCache = arc.ARCCache[cid.Cid, ipfsRecord]
	ipfsDirCache  = arc.ARCCache[cid.Cid, []filesystem.StreamDirEntry]
	// FS implements [fs.FS] and [filesystem] extensions.
	FS struct {
		ctx        context.Context
		cancel     context.CancelFunc
		core       coreiface.CoreAPI
		resolver   resolver.Resolver
		nodeCache  *ipfsNodeCache
		dirCache   *ipfsDirCache
		info       intp.NodeInfo
		apiTimeout time.Duration
		linkLimit  uint
	}
	ipfsDirectory struct {
		stream *intp.EntryStream
		info   *intp.NodeInfo
		err    error
		cid    cid.Cid
	}
)

// ID defines the identifier of this system.
const ID filesystem.ID = "IPFS"

func (*FS) ID() filesystem.ID { return ID }

// New constructs an [FS] using the defaults listed in the pkg constants.
// A list of [Option] values can be provided to change these defaults as desired.
func New(core coreiface.CoreAPI, options ...Option) (*FS, error) {
	var (
		fsys = &FS{
			info: intp.NodeInfo{
				Name_:    filesystem.Root,
				ModTime_: time.Now(),
				Mode_:    fs.ModeDir | DefaultPermissions,
			},
			core:       core,
			apiTimeout: DefaultAPITimeout,
			linkLimit:  DefaultLinkLimit,
		}
		settings = settings{
			FS:               fsys,
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
	fsys.resolver = intp.NewPathResolver(fsys.getNode)
	return fsys, nil
}

func (settings *settings) fillInDefaults() error {
	if fsys := settings.FS; fsys.ctx == nil {
		fsys.ctx, fsys.cancel = context.WithCancel(context.Background())
	}
	if settings.defaultNodeCache {
		if err := settings.initNodeCache(DefaultNodeCacheCount); err != nil {
			return err
		}
	}
	if settings.defaultDirCache {
		if err := settings.initDirectoryCache(DefaultNodeCacheCount); err != nil {
			return err
		}
	}
	return nil
}

func (settings *settings) initNodeCache(count int) error {
	nodeCache, err := arc.NewARC[cid.Cid, ipfsRecord](count)
	if err != nil {
		return err
	}
	settings.nodeCache = nodeCache
	return nil
}

func (settings *settings) initDirectoryCache(count int) error {
	dirCache, err := arc.NewARC[cid.Cid, []filesystem.StreamDirEntry](count)
	if err != nil {
		return err
	}
	settings.dirCache = dirCache
	return nil
}

func (fsys *FS) Close() error {
	fsys.cancel()
	return nil
}

func (fsys *FS) Lstat(name string) (fs.FileInfo, error) {
	const op = "lstat"
	info, _, err := fsys.lstat(op, name)
	return info, err
}

func (fsys *FS) lstat(op, name string) (fs.FileInfo, cid.Cid, error) {
	if name == filesystem.Root {
		return &fsys.info, cid.Cid{}, nil
	}
	cid, err := fsys.toCID(op, name)
	if err != nil {
		return nil, cid, err
	}
	info, err := fsys.getInfo(name, cid)
	if err != nil {
		const kind = fserrors.IO
		return nil, cid, fserrors.New(op, name, err, kind)
	}
	return info, cid, nil
}

func (fsys *FS) Stat(name string) (fs.FileInfo, error) {
	const depth = 0
	return fsys.stat(name, depth)
}

func (fsys *FS) stat(name string, depth uint) (fs.FileInfo, error) {
	const op = "stat"
	info, cid, err := fsys.lstat(op, name)
	if err != nil {
		return nil, err
	}
	if isLink := info.Mode()&fs.ModeSymlink != 0; !isLink {
		return info, nil
	}
	if depth++; depth >= fsys.linkLimit {
		return nil, intp.LinkLimitError(op, name, fsys.linkLimit)
	}
	target, err := fsys.resolveCIDSymlink(op, name, cid)
	if err != nil {
		return nil, err
	}
	return fsys.stat(target, depth)
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
	return fsys.resolveCIDSymlink(op, name, cid)
}

func (fsys *FS) resolveCIDSymlink(op, name string, cid cid.Cid) (string, error) {
	var (
		ufs         = fsys.core.Unixfs()
		ctx, cancel = fsys.nodeContext()
	)
	defer cancel()
	const allowedPrefix = "/ipfs/"
	return intp.GetUnixFSLink(ctx, op, name, ufs, cid, allowedPrefix)
}

func (fsys *FS) toCID(op, goPath string) (cid.Cid, error) {
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
	nodeCID, err := fsys.ResolvePath(goPath)
	if err != nil {
		kind := intp.ResolveErrKind(err)
		return cid.Cid{}, fserrors.New(op, goPath, err, kind)
	}
	return nodeCID, nil
}

func cidErrKind(err error) fserrors.Kind {
	if errors.Is(err, multibase.ErrUnsupportedEncoding) {
		return fserrors.NotExist
	}
	return fserrors.IO
}

func (fsys *FS) getInfo(name string, cid cid.Cid) (*intp.NodeInfo, error) {
	cache := fsys.nodeCache
	if cacheDisabled := cache == nil; cacheDisabled {
		return fsys.fetchInfo(name, cid)
	}
	record, _ := cache.Get(cid)
	if info := record.NodeInfo; info != nil {
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
		info     = intp.NodeInfo{
			Name_:    name,
			ModTime_: rootInfo.ModTime_,
			Mode_:    rootInfo.Mode_.Perm(),
		}
	)
	if err := intp.StatNode(node, &info); err != nil {
		return nil, err
	}
	record.NodeInfo = &info
	cache.Add(cid, record)
	return &info, nil
}

func (fsys *FS) fetchInfo(name string, cid cid.Cid) (*intp.NodeInfo, error) {
	node, err := fsys.getNode(cid)
	if err != nil {
		return nil, err
	}
	var (
		rootInfo = fsys.info
		info     = intp.NodeInfo{
			Name_:    name,
			ModTime_: rootInfo.ModTime_,
			Mode_:    rootInfo.Mode_.Perm(),
		}
	)
	if err := intp.StatNode(node, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (fsys *FS) getNode(cid cid.Cid) (ipld.Node, error) {
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

func (fsys *FS) fetchNode(cid cid.Cid) (ipld.Node, error) {
	ctx, cancel := fsys.nodeContext()
	defer cancel()
	return fsys.core.Dag().Get(ctx, cid)
}

func (fsys *FS) nodeContext() (context.Context, context.CancelFunc) {
	var (
		ctx     = fsys.ctx
		timeout = fsys.apiTimeout
	)
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func (fsys *FS) ResolvePath(goPath string) (cid.Cid, error) {
	var (
		ctx          = fsys.ctx
		resolver     = fsys.resolver
		iPath        = ipath.FromString(goPath)
		leaf, _, err = resolver.ResolveToLastNode(ctx, iPath)
	)
	return leaf, err
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
	info, err := fsys.getInfo(name, cid)
	if err != nil {
		const kind = fserrors.IO
		return nil, fserrors.New(op, name, err, kind)
	}
	switch typ := info.Mode_.Type(); typ {
	case fs.FileMode(0):
		return fsys.openFile(cid, info)
	case fs.ModeDir:
		return fsys.openDir(cid, info)
	case fs.ModeSymlink:
		if depth++; depth >= fsys.linkLimit {
			return nil, intp.LinkLimitError(op, name, fsys.linkLimit)
		}
		target, err := fsys.resolveCIDSymlink(op, name, cid)
		if err != nil {
			return nil, err
		}
		return fsys.open(target, depth)
	default:
		return nil, fmt.Errorf(
			"%w got: \"%s\" want: regular file or directory",
			intp.ErrUnexpectedType, intp.FSTypeName(typ),
		)
	}
}

func (fsys *FS) openDir(cid cid.Cid, info *intp.NodeInfo) (fs.File, error) {
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
		stream: &intp.EntryStream{
			Context: dirCtx, CancelFunc: cancel,
			Ch: entries,
		},
	}, nil
}

func (fsys *FS) getEntries(ctx context.Context, cid cid.Cid, info *intp.NodeInfo) (<-chan filesystem.StreamDirEntry, error) {
	cache := fsys.dirCache
	if cacheDisabled := cache == nil; cacheDisabled {
		return fsys.fetchEntries(ctx, cid, info)
	}
	if entries, _ := cache.Get(cid); entries != nil {
		return intp.GenerateEntryChan(ctx, entries), nil
	}
	return fsys.fetchAndCacheEntries(ctx, cid, info)
}

func (fsys *FS) fetchAndCacheEntries(ctx context.Context, cid cid.Cid, info *intp.NodeInfo) (<-chan filesystem.StreamDirEntry, error) {
	fetchFn := func(ctx context.Context) (<-chan filesystem.StreamDirEntry, error) {
		return fsys.fetchEntries(ctx, cid, info)
	}
	// TODO: Is there a way for us to get the entry count
	// of a UFS directory before we read it from the channel?
	// Alternatively, we could store an average on fsys
	// and use it to pre-allocate the accumulator. Bad idea?
	var accumulator []filesystem.StreamDirEntry
	return intp.AccumulateAndRelay(
		fsys.ctx, ctx,
		fetchFn, accumulator,
		func(accumulator []filesystem.StreamDirEntry) {
			if accumulator != nil {
				fsys.dirCache.Add(cid, accumulator)
			}
		})
}

func (fsys *FS) fetchEntries(ctx context.Context, cid cid.Cid, info *intp.NodeInfo) (<-chan filesystem.StreamDirEntry, error) {
	var (
		api          = fsys.core.Unixfs()
		path         = corepath.IpfsPath(cid)
		entries, err = api.Ls(ctx, path, coreoptions.Unixfs.ResolveChildren(true))
	)
	if err != nil {
		return nil, err
	}
	var (
		modTime     = info.ModTime_
		permissions = info.Mode_.Perm()
		converted   = intp.NewStreamChan(entries)
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
				case converted <- &intp.CoreDirEntry{
					DirEntry:    entry,
					ModTime_:    modTime,
					Permissions: permissions,
				}:
				case <-ctx.Done():
					intp.DrainThenSendErr(converted, ctx.Err())
					return
				}
			case <-ctx.Done():
				intp.DrainThenSendErr(converted, ctx.Err())
				return
			}
		}
	}()
	return converted, nil
}

func (fsys *FS) openFile(cid cid.Cid, info *intp.NodeInfo) (fs.File, error) {
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
	return -1, fserrors.New(op, id.info.Name_, filesystem.ErrIsDir, fserrors.IsDir)
}

func (id *ipfsDirectory) StreamDir() <-chan filesystem.StreamDirEntry {
	const op = "streamdir"
	if stream := id.stream; stream != nil {
		return stream.Ch
	}
	errs := make(chan filesystem.StreamDirEntry, 1)
	errs <- intp.NewErrorEntry(
		fserrors.New(op, id.info.Name_, fs.ErrClosed, fserrors.Closed),
	)
	close(errs)
	return errs
}

func (id *ipfsDirectory) ReadDir(count int) ([]fs.DirEntry, error) {
	const op = "readdir"
	if err := id.err; err != nil {
		return nil, err
	}
	stream := id.stream
	if stream == nil {
		return nil, fserrors.New(op, id.info.Name_, fs.ErrClosed, fserrors.Closed)
	}
	var (
		ctx       = stream.Context
		entryChan = stream.Ch
	)
	entries, err := intp.ReadEntries(ctx, entryChan, count)
	if err != nil {
		err = intp.ReaddirErr(op, id.info.Name_, err)
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
	return fserrors.New(op, id.info.Name_, fs.ErrClosed, fserrors.Closed)
}
