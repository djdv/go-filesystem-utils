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
	"github.com/djdv/go-filesystem-utils/internal/generic"
	coreiface "github.com/ipfs/boxo/coreiface"
	corepath "github.com/ipfs/boxo/coreiface/path"
	files "github.com/ipfs/boxo/files"
	mdag "github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs"
	unixpb "github.com/ipfs/boxo/ipld/unixfs/pb"
	"github.com/ipfs/boxo/path/resolver"
	"github.com/ipfs/go-cid"
	ipfscmds "github.com/ipfs/go-ipfs-cmds"
	cbor "github.com/ipfs/go-ipld-cbor"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/multiformats/go-multibase"
)

type (
	contextSetter[T any] interface {
		*T
		setContext(context.Context)
	}
	nodeTimeoutSetter[T any] interface {
		*T
		setNodeTimeout(time.Duration)
	}
	linkLimitSetter[T any] interface {
		*T
		setLinkLimit(uint)
	}
	permissionSetter[T any] interface {
		*T
		setPermissions(fs.FileMode)
	}
	dagSetter[T any] interface {
		*T
		setDag(coreiface.APIDagService)
	}
	nodeInfo struct {
		modTime time.Time
		name    string
		size    int64
		mode    fs.FileMode
	}
	emptyRoot      struct{ info *nodeInfo }
	ctxChan[T any] struct {
		context.Context
		context.CancelFunc
		ch <-chan T
	}
	entryStream = ctxChan[filesystem.StreamDirEntry]
	errorEntry  struct {
		fs.DirEntry // Should always be nil.
		error
	}
	coreDirEntry struct {
		coreiface.DirEntry
		modTime     time.Time
		permissions fs.FileMode
	}
	symlinkRFS interface {
		filesystem.LinkStater
		filesystem.LinkReader
	}
	symlinkFS interface {
		symlinkRFS
		filesystem.LinkMaker
	}
)

const (
	errUnexpectedType = generic.ConstError("unexpected type")
	errEmptyLink      = generic.ConstError("empty link target")
	errRootLink       = generic.ConstError("root is not a symlink")
	executeAll        = filesystem.ExecuteUser | filesystem.ExecuteGroup | filesystem.ExecuteOther
	readAll           = filesystem.ReadUser | filesystem.ReadGroup | filesystem.ReadOther
)

var _ fs.FileInfo = (*nodeInfo)(nil)

func (ee errorEntry) Error() error { return ee.error }

func newErrorEntry(err error) filesystem.StreamDirEntry {
	return errorEntry{error: err}
}

func WithContext[
	OT generic.OptionFunc[T],
	T any,
	I contextSetter[T],
](ctx context.Context,
) OT {
	return func(settings *T) error {
		any(settings).(I).setContext(ctx)
		return nil
	}
}

// WithNodeTimeout sets a timeout duration to use
// when communicating with the IPFS API/node.
// If <= 0, operations will not time out,
// and will remain pending until the file system is closed.
func WithNodeTimeout[
	OT generic.OptionFunc[T],
	T any,
	I nodeTimeoutSetter[T],
](timeout time.Duration,
) OT {
	return func(settings *T) error {
		any(settings).(I).setNodeTimeout(timeout)
		return nil
	}
}

// WithLinkLimit sets the maximum amount of times an
// operation will resolve a symbolic link chain,
// before it returns a recursion error.
func WithLinkLimit[
	OT generic.OptionFunc[T],
	T any,
	I linkLimitSetter[T],
](limit uint,
) OT {
	return func(settings *T) error {
		any(settings).(I).setLinkLimit(limit)
		return nil
	}
}

func WithPermissions[
	OT generic.OptionFunc[T],
	T any,
	I permissionSetter[T],
](permissions fs.FileMode,
) OT {
	return func(mode *T) error {
		any(mode).(I).setPermissions(permissions)
		return nil
	}
}

// WithDagService supplies a dag service to
// use to add support for various write operations.
func WithDagService[
	OT generic.OptionFunc[T],
	T any,
	I dagSetter[T],
](dag coreiface.APIDagService,
) OT {
	return func(mode *T) error {
		any(mode).(I).setDag(dag)
		return nil
	}
}

func (ni *nodeInfo) Name() string       { return ni.name }
func (ni *nodeInfo) Size() int64        { return ni.size }
func (ni *nodeInfo) Mode() fs.FileMode  { return ni.mode }
func (ni *nodeInfo) ModTime() time.Time { return ni.modTime }
func (ni *nodeInfo) IsDir() bool        { return ni.Mode().IsDir() }
func (ni *nodeInfo) Sys() any           { return ni }

func (cde *coreDirEntry) Name() string               { return cde.DirEntry.Name }
func (cde *coreDirEntry) IsDir() bool                { return cde.Type().IsDir() }
func (cde *coreDirEntry) Info() (fs.FileInfo, error) { return cde, nil }
func (cde *coreDirEntry) Size() int64                { return int64(cde.DirEntry.Size) }
func (cde *coreDirEntry) ModTime() time.Time         { return cde.modTime }
func (cde *coreDirEntry) Mode() fs.FileMode          { return cde.Type() | cde.permissions }
func (cde *coreDirEntry) Sys() any                   { return cde }
func (cde *coreDirEntry) Error() error               { return cde.DirEntry.Err }
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

func (er emptyRoot) Stat() (fs.FileInfo, error) { return er.info, nil }
func (emptyRoot) Close() error                  { return nil }
func (emptyRoot) Read([]byte) (int, error) {
	const op = "emptyRoot.Read"
	return -1, fserrors.New(op, filesystem.Root, filesystem.ErrIsDir, fserrors.IsDir)
}

func (emptyRoot) ReadDir(count int) ([]fs.DirEntry, error) {
	if count > 0 {
		return nil, io.EOF
	}
	return nil, nil
}

func validatePath(op, name string) error {
	if fs.ValidPath(name) {
		return nil
	}
	return fserrors.New(op, name, fs.ErrInvalid, fserrors.InvalidItem)
}

func statNode(node ipld.Node, info *nodeInfo) error {
	switch typedNode := node.(type) {
	case *mdag.ProtoNode:
		return statProto(typedNode, info)
	case *cbor.Node:
		return statCbor(typedNode, info)
	default:
		return statGeneric(node, info)
	}
}

func statProto(node *mdag.ProtoNode, info *nodeInfo) error {
	ufsNode, err := unixfs.ExtractFSNode(node)
	if err != nil {
		return err
	}
	info.size = int64(ufsNode.FileSize())
	switch ufsNode.Type() {
	case unixpb.Data_Directory, unixpb.Data_HAMTShard:
		info.mode |= fs.ModeDir
	case unixpb.Data_Symlink:
		info.mode |= fs.ModeSymlink
	case unixpb.Data_File, unixpb.Data_Raw:
	// NOOP:  stat.mode |= fs.FileMode(0)
	default:
		info.mode |= fs.ModeIrregular
	}
	return nil
}

func statGeneric(node ipld.Node, info *nodeInfo) error {
	nodeStat, err := node.Stat()
	if err != nil {
		return err
	}
	info.size = int64(nodeStat.CumulativeSize)
	return nil
}

func generateEntryChan(ctx context.Context, values []filesystem.StreamDirEntry) <-chan filesystem.StreamDirEntry {
	out := make(chan filesystem.StreamDirEntry, 1)
	go func() {
		defer close(out)
		for _, value := range values {
			if err := ctx.Err(); err != nil {
				drainThenSendErr(out, err)
				return
			}
			select {
			case out <- value:
			case <-ctx.Done():
				drainThenSendErr(out, ctx.Err())
				return
			}
		}
	}()
	return out
}

// readEntries handles different behaviour expected by
// [fs.ReadDirFile].
// Specifically in regard to the returned values.
func readEntries(ctx context.Context,
	entries <-chan filesystem.StreamDirEntry, count int,
) (requested []fs.DirEntry, err error) {
	readAll := count <= 0
	if readAll {
		requested = make([]fs.DirEntry, 0, cap(entries))
	} else {
		const upperBound = 16
		requested = make([]fs.DirEntry, 0, generic.Min(count, upperBound))
	}
	requested, err = readEntriesCount(ctx, entries, requested, count)
	if err != nil && !readAll {
		requested = nil
	}
	return
}

// readEntriesCount does the actual readdir operation.
// It always returns `requested`.
func readEntriesCount(ctx context.Context,
	entries <-chan filesystem.StreamDirEntry,
	requested []fs.DirEntry,
	count int,
) ([]fs.DirEntry, error) {
	for {
		select {
		case entry, ok := <-entries:
			if !ok {
				if len(requested) == 0 {
					return requested, io.EOF
				}
				return requested, nil
			}
			if err := entry.Error(); err != nil {
				return requested, err
			}
			requested = append(requested, entry)
			if count--; count == 0 {
				return requested, nil
			}
		case <-ctx.Done():
			return requested, ctx.Err()
		}
	}
}

func readdirErr(op, path string, err error) error {
	if err == io.EOF {
		return err
	}
	return fserrors.New(op, path, err, fserrors.IO)
}

func cidErrKind(err error) fserrors.Kind {
	if errors.Is(err, multibase.ErrUnsupportedEncoding) {
		return fserrors.NotExist
	}
	return fserrors.IO
}

func resolveErrKind(err error) fserrors.Kind {
	var resolveErr resolver.ErrNoLink
	if errors.As(err, &resolveErr) {
		return fserrors.NotExist
	}
	// XXX: Upstream doesn't define error values
	// to compare against. We have to fallback to strings.
	// This could break at any time.
	//
	// TODO: split this function?
	// 1 for errors returned from core API,
	// 1 for ipld only?
	const (
		notFoundCore = "no link named"
		// Specifically for OS sidecar files
		// that will not be valid CIDs.
		// E.g. `/$ns/desktop.ini`, `/$ns/.DS_Store`, et al.
		invalid = "invalid path"
	)
	var cmdsErr *ipfscmds.Error
	if errors.As(err, &cmdsErr) &&
		cmdsErr.Code == ipfscmds.ErrNormal &&
		(strings.Contains(cmdsErr.Message, notFoundCore) ||
			strings.Contains(cmdsErr.Message, invalid)) {
		return fserrors.NotExist
	}
	const notFoundIPLD = "no link by that name"
	if strings.Contains(err.Error(), notFoundIPLD) {
		return fserrors.NotExist
	}
	return fserrors.IO
}

func newStreamChan[T any](ch <-chan T) chan filesystem.StreamDirEntry {
	// +1 relay must account for ctx error.
	return make(chan filesystem.StreamDirEntry, cap(ch)+1)
}

// accumulateRelayClose accumulates and relays
// from `entries`.
// `ctx` applies only to sends on `relay`.
// Regardless of cancellation, values will be
// received from `entries` and accumulated until it's closed.
// `relay` must have a cap of at least 1
// and will (eventually) be closed by this call.
func accumulateRelayClose(ctx context.Context,
	entries <-chan filesystem.StreamDirEntry,
	relay chan filesystem.StreamDirEntry,
	accumulator []filesystem.StreamDirEntry,
) (sawError bool, _ []filesystem.StreamDirEntry) {
	var (
		sent     int
		unsent   []filesystem.StreamDirEntry
		canceled func() bool
		relayFn  func()
	)
	canceled = func() bool {
		if err := ctx.Err(); err != nil {
			drainThenSendErr(relay, err)
			close(relay)
			canceled = func() bool { return true }
			return true
		}
		return false
	}
	relayFn = func() {
		if canceled() {
			relayFn, unsent = func() {}, nil
			return
		}
		unsent = accumulator[sent:]
		select {
		case relay <- unsent[0]:
			sent++
			unsent = unsent[1:]
		default: // Don't wait on relay; keep caching.
		}
	}
	for entry := range entries {
		if entry.Error() != nil {
			sawError = true
		}
		accumulator = append(accumulator, entry)
		relayFn()
	}
	if canceled() {
		return sawError, accumulator
	}
	if len(unsent) == 0 {
		close(relay)
		return sawError, accumulator
	}
	// NOTE: `unsent` is slice of `accumulator`.
	clone := generic.CloneSlice(unsent) // (which could be modified by the caller.)
	go func() {
		defer close(relay)
		for _, entry := range clone {
			select {
			case relay <- entry:
			case <-ctx.Done():
				drainThenSendErr(relay, ctx.Err())
				return
			}
		}
	}()
	return sawError, accumulator
}

func drainThenSendErr(ch chan filesystem.StreamDirEntry, err error) {
	generic.DrainBuffer(ch)
	ch <- newErrorEntry(err)
}

func fsTypeName(mode fs.FileMode) string {
	switch mode.Type() {
	case fs.FileMode(0):
		return "regular"
	case fs.ModeDir:
		return "directory"
	case fs.ModeSymlink:
		return "symbolic link"
	case fs.ModeNamedPipe:
		return "named pipe"
	case fs.ModeSocket:
		return "socket"
	case fs.ModeDevice:
		return "device"
	case fs.ModeCharDevice:
		return "character device"
	case fs.ModeIrregular:
		fallthrough
	default:
		return "irregular"
	}
}

func makeAndAddLink(ctx context.Context, target string, dag coreiface.APIDagService) (cid.Cid, error) {
	dagData, err := unixfs.SymlinkData(target)
	if err != nil {
		return cid.Cid{}, err
	}
	dagNode := mdag.NodeWithData(dagData)
	if err := dag.Add(ctx, dagNode); err != nil {
		return cid.Cid{}, err
	}
	return dagNode.Cid(), nil
}

func getUnixFSLink(ctx context.Context,
	op, name string,
	ufs coreiface.UnixfsAPI, cid cid.Cid,
	allowedPrefix string,
) (string, error) {
	cPath := corepath.IpfsPath(cid)
	link, err := ufs.Get(ctx, cPath)
	if err != nil {
		const kind = fserrors.IO
		return "", fserrors.New(op, name, err, kind)
	}
	return resolveNodeLink(op, name, link, allowedPrefix)
}

func resolveNodeLink(op, name string, node files.Node, prefix string) (string, error) {
	target, err := readNodeLink(op, name, node)
	if err != nil {
		return "", err
	}
	// We allow 2 kinds of absolute links:
	// 1) File system's root
	// 2) Paths matching an explicitly allowed prefix
	if strings.HasPrefix(target, prefix) {
		target = strings.TrimPrefix(target, prefix)
		return path.Clean(target), nil
	}
	switch target {
	case "/":
		return filesystem.Root, nil
	case "..":
		name = path.Dir(name)
		fallthrough
	case ".":
		return path.Dir(name), nil
	}
	if target[0] == '/' {
		const (
			err  = generic.ConstError("link target must be relative")
			kind = fserrors.InvalidItem
		)
		pair := fmt.Sprintf(
			`%s -> %s`,
			name, target,
		)
		return "", fserrors.New(op, pair, err, kind)
	}
	if target = path.Join("/"+name, target); target == "/" {
		target = filesystem.Root
	}
	target = strings.TrimPrefix(target, "/")
	return target, nil
}
