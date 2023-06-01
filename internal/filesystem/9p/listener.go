package p9

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	p9net "github.com/djdv/go-filesystem-utils/internal/net/9p"
	perrors "github.com/djdv/p9/errors"
	"github.com/djdv/p9/fsimpl/templatefs"
	"github.com/djdv/p9/p9"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	Listener struct {
		directory
		listenerShared
	}
	listenerSettings struct {
		metaOptions      []metadataOption
		directoryOptions []DirectoryOption
		directorySettings
		channelSettings
	}
	ListenerOption func(*listenerSettings) error
	listenerShared struct {
		emitter        *chanEmitter[manet.Listener]
		path           ninePath
		cleanupEmpties bool
	}
	protocolDir struct {
		directory
		*linkSync
		*listenerShared
		protocol *multiaddr.Protocol
	}
	valueDir struct {
		directory
		*linkSync
		*listenerShared
		component  *multiaddr.Component
		connDirMu  *sync.Mutex
		connDirPtr **connDir
		connIndex  *atomic.Uintptr
	}
	listenerFile struct {
		templatefs.NoopFile
		metadata
		Listener manet.Listener
		*linkSync
		io.ReaderAt
		openFlags
	}
	listenerCloser struct {
		manet.Listener
		closeFn func() error
	}
	connTracker struct {
		parent *valueDir
		manet.Listener
	}
	connDir struct {
		directory
		path ninePath
	}
	connFile struct {
		templatefs.NoopFile
		metadata
		trackedConn
		io.ReaderAt
		*linkSync
		connID uintptr
		openFlags
	}
	trackedConn interface {
		manet.Conn
		p9net.TrackedIO
	}
	connCloser struct {
		trackedConn
		closeFn func() error
	}
	ConnInfo struct {
		LastRead  time.Time           `json:"lastRead"`
		LastWrite time.Time           `json:"lastWrite"`
		Local     multiaddr.Multiaddr `json:"local"`
		Remote    multiaddr.Multiaddr `json:"remote"`
		ID        uintptr             `json:"#"`
	}
)

const (
	listenerFileName    = "listener"
	connectionsFileName = "connections"
)

func NewListener(ctx context.Context, options ...ListenerOption) (p9.QID, *Listener, <-chan manet.Listener, error) {
	var settings listenerSettings
	if err := parseOptions(&settings, options...); err != nil {
		return p9.QID{}, nil, nil, err
	}
	metadata, err := makeMetadata(p9.ModeDirectory, settings.metaOptions...)
	if err != nil {
		return p9.QID{}, nil, nil, err
	}
	directoryOptions := append(
		settings.directoryOptions,
		WithoutRename[DirectoryOption](true),
	)
	qid, directory, err := NewDirectory(directoryOptions...)
	if err != nil {
		return p9.QID{}, nil, nil, err
	}
	var (
		emitter   = makeChannelEmitter[manet.Listener](ctx, settings.channelSettings.buffer)
		listeners = emitter.ch
		listener  = &Listener{
			directory: directory,
			listenerShared: listenerShared{
				path:           metadata.ninePath,
				emitter:        emitter,
				cleanupEmpties: settings.cleanupElements,
			},
		}
	)
	return qid, listener, listeners, nil
}

// TODO: [Ame] English.
// Listen tries to listen on the provided [Multiaddr].
// If successful, the [Multiaddr] is mapped as a directory,
// inheriting permissions from parent directories all the way down.
// The passed permissions are used for the final API file.
func Listen(listener p9.File, maddr multiaddr.Multiaddr, permissions p9.FileMode) error {
	var (
		_, names = splitMaddr(maddr)
		uid      = p9.NoUID
		gid      = p9.NoGID
	)
	valueDir, err := MkdirAll(listener, names, permissions, uid, gid)
	if err != nil {
		return err
	}
	permissions ^= ExecuteOther | ExecuteGroup | ExecuteUser
	_, err = valueDir.Mknod(listenerFileName, permissions, 0, 0, p9.NoUID, p9.NoGID)
	if cErr := valueDir.Close(); cErr != nil {
		return errors.Join(err, cErr)
	}
	return err
}

// GetListeners returns a slice of maddrs that correspond to
// active listeners contained within the `listener` file.
func GetListeners(listener p9.File) ([]multiaddr.Multiaddr, error) {
	var (
		ctx, cancel = context.WithCancel(context.Background())
		results     = getListeners(ctx, listener)
	)
	defer cancel()
	return aggregateResults(cancel, results)
}

func getListeners(ctx context.Context, listener p9.File) <-chan maddrResult {
	return mapDirPipeline(ctx, listener, listenerPipeline)
}

func listenerPipeline(ctx context.Context,
	listener p9.File,
	wg *sync.WaitGroup, results chan<- maddrResult,
) {
	defer wg.Done()
	processFile := func(result fileResult) {
		defer wg.Done()
		if err := result.error; err != nil {
			sendResult(ctx, results, maddrResult{error: err})
			return
		}
		var (
			listenerFile = result.value
			maddr, err   = parseListenerFile(listenerFile)
		)
		if cErr := listenerFile.Close(); cErr != nil {
			err = errors.Join(err, cErr)
		}
		sendResult(ctx, results, maddrResult{value: maddr, error: err})
	}
	for result := range findFiles(ctx, listener, listenerFileName) {
		wg.Add(1)
		go processFile(result)
	}
}

func parseListenerFile(file p9.File) (multiaddr.Multiaddr, error) {
	maddrBytes, err := ReadAll(file)
	if err != nil {
		return nil, err
	}
	return multiaddr.NewMultiaddr(string(maddrBytes))
}

// GetConnections returns a slice of info that corresponds to
// active connections contained within the `listener` file.
func GetConnections(listener p9.File) ([]ConnInfo, error) {
	var (
		ctx, cancel = context.WithCancel(context.Background())
		results     = getConnections(ctx, listener)
	)
	defer cancel()
	return aggregateResults(cancel, results)
}

func getConnections(ctx context.Context, listener p9.File) <-chan connInfoResult {
	return mapDirPipeline(ctx, listener, connectionPipeline)
}

func connectionPipeline(ctx context.Context,
	listener p9.File,
	wg *sync.WaitGroup, results chan<- connInfoResult,
) {
	defer wg.Done()
	processFile := func(result fileResult) {
		defer wg.Done()
		if err := result.error; err != nil {
			sendResult(ctx, results, connInfoResult{error: err})
			return
		}
		connDir := result.value
		defer func() {
			if err := connDir.Close(); err != nil {
				sendResult(ctx, results, connInfoResult{error: err})
			}
		}()
		for fileRes := range flattenDir(ctx, connDir) {
			if err := fileRes.error; err != nil {
				sendResult(ctx, results, connInfoResult{error: err})
				continue
			}
			var (
				connFile  = fileRes.value
				info, err = parseConnFile(connFile)
			)
			if cErr := connFile.Close(); cErr != nil {
				err = errors.Join(err, cErr)
			}
			sendResult(ctx, results, connInfoResult{value: info, error: err})
		}
	}
	for result := range findFiles(ctx, listener, connectionsFileName) {
		wg.Add(1)
		go processFile(result)
	}
}

func parseConnFile(file p9.File) (ConnInfo, error) {
	connData, err := ReadAll(file)
	if err != nil {
		return ConnInfo{}, err
	}
	var info ConnInfo
	return info, json.Unmarshal(connData, &info)
}

func (ld *Listener) Walk(names []string) ([]p9.QID, p9.File, error) {
	qids, file, err := ld.directory.Walk(names)
	if len(names) == 0 {
		file = &Listener{
			directory:      file,
			listenerShared: ld.listenerShared,
		}
	}
	return qids, file, err
}

func (ld *Listener) Link(file p9.File, name string) error {
	var (
		_, pOk = file.(*protocolDir)
		_, vOk = file.(*valueDir)
		ok     = pOk || vOk
	)
	if !ok {
		return fmt.Errorf("%w - unexpected file type", perrors.EACCES)
	}
	return ld.directory.Link(file, name)
}

func (ld *Listener) Mkdir(name string, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, error) {
	protocol, err := getProtocol(name)
	if err != nil {
		return p9.QID{}, fmt.Errorf("%w - %s", perrors.EIO, err)
	}
	link, err := newLinkSync(
		WithParent[linkOption](ld, name),
		WithoutRename[linkOption](true),
	)
	if err != nil {
		return p9.QID{}, err
	}
	qid, directory, err := ld.mkdir(ld.directory,
		name, permissions, uid, gid,
	)
	if err != nil {
		return p9.QID{}, err
	}
	protoDir := &protocolDir{
		listenerShared: &ld.listenerShared,
		linkSync:       link,
		directory:      directory,
		protocol:       protocol,
	}
	return qid, ld.directory.Link(protoDir, name)
}

func (ls *listenerShared) mkdir(directory p9.File, name string,
	permissions p9.FileMode, uid p9.UID, gid p9.GID,
) (p9.QID, p9.File, error) {
	uid, gid, err := mkPreamble(directory, name, uid, gid)
	if err != nil {
		return p9.QID{}, nil, err
	}
	cleanup := ls.cleanupEmpties
	return NewDirectory(
		WithPath[DirectoryOption](ls.path),
		WithPermissions[DirectoryOption](permissions),
		WithUID[DirectoryOption](uid),
		WithGID[DirectoryOption](gid),
		WithParent[DirectoryOption](directory, name),
		UnlinkWhenEmpty[DirectoryOption](cleanup),
		UnlinkEmptyChildren[DirectoryOption](cleanup),
	)
}

func (pd *protocolDir) Walk(names []string) ([]p9.QID, p9.File, error) {
	qids, file, err := pd.directory.Walk(names)
	if len(names) == 0 {
		file = &protocolDir{
			directory:      file,
			listenerShared: pd.listenerShared,
			linkSync:       pd.linkSync,
			protocol:       pd.protocol,
		}
	}
	return qids, file, err
}

func (pd *protocolDir) Renamed(newDir p9.File, newName string) {
	pd.directory.Renamed(newDir, newName)
}

func (pd *protocolDir) Link(file p9.File, name string) error {
	if _, ok := file.(*valueDir); !ok {
		return fmt.Errorf("%w - unexpected file type", perrors.EACCES)
	}
	return pd.directory.Link(file, name)
}

func (pd *protocolDir) Mkdir(name string, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, error) {
	var component *multiaddr.Component
	if !pd.protocol.Path {
		var (
			err      error
			protocol = pd.protocol.Name
		)
		if component, err = multiaddr.NewComponent(protocol, name); err != nil {
			return p9.QID{}, err
		}
	}
	qid, directory, err := pd.mkdir(pd.directory,
		name, permissions, uid, gid,
	)
	if err != nil {
		return p9.QID{}, err
	}
	link, err := newLinkSync(WithParent[linkOption](pd, name))
	if err != nil {
		return p9.QID{}, err
	}
	newDir := &valueDir{
		listenerShared: pd.listenerShared,
		linkSync:       link,
		directory:      directory,
		component:      component,
		connDirMu:      new(sync.Mutex),
		connDirPtr:     new(*connDir),
		connIndex:      new(atomic.Uintptr),
	}
	return qid, pd.directory.Link(newDir, name)
}

func (vd *valueDir) Walk(names []string) ([]p9.QID, p9.File, error) {
	qids, file, err := vd.directory.Walk(names)
	if len(names) == 0 {
		file = &valueDir{
			directory:      file,
			listenerShared: vd.listenerShared,
			linkSync:       vd.linkSync,
			component:      vd.component,
			connDirMu:      vd.connDirMu,
			connDirPtr:     vd.connDirPtr,
			connIndex:      vd.connIndex,
		}
	}
	return qids, file, err
}

func (vd *valueDir) Renamed(newDir p9.File, newName string) {
	vd.linkSync.Renamed(newDir, newName)
}

func (vd *valueDir) Link(file p9.File, name string) error {
	var (
		_, pOk = file.(*protocolDir)
		_, vOk = file.(*valueDir)
		_, cOk = file.(*connDir)
		_, fOk = file.(*listenerFile)
		ok     = pOk || vOk || cOk || fOk
	)
	if !ok {
		return fmt.Errorf("%w - unexpected file type", perrors.EACCES)
	}
	return vd.directory.Link(file, name)
}

func (vd *valueDir) Mkdir(name string, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, error) {
	link, err := newLinkSync(WithParent[linkOption](vd, name))
	if err != nil {
		return p9.QID{}, err
	}
	if isPathType := vd.component == nil; isPathType {
		qid, directory, err := vd.mkdir(vd.directory,
			name, permissions, uid, gid,
		)
		if err != nil {
			return p9.QID{}, err
		}
		valueDir := &valueDir{
			directory:      directory,
			listenerShared: vd.listenerShared,
			linkSync:       link,
			connDirMu:      vd.connDirMu,
			connDirPtr:     vd.connDirPtr,
			connIndex:      vd.connIndex,
		}
		return qid, vd.directory.Link(valueDir, name)
	}
	protocol, err := getProtocol(name)
	if err != nil {
		return p9.QID{}, fmt.Errorf("%w - %s", perrors.EIO, err)
	}
	qid, directory, err := vd.mkdir(vd.directory,
		name, permissions, uid, gid,
	)
	if err != nil {
		return p9.QID{}, err
	}
	protoDir := &protocolDir{
		listenerShared: vd.listenerShared,
		linkSync:       link,
		directory:      directory,
		protocol:       protocol,
	}
	return qid, vd.directory.Link(protoDir, name)
}

func (vd *valueDir) Create(name string, flags p9.OpenFlags,
	permissions p9.FileMode, uid p9.UID, gid p9.GID,
) (p9.File, p9.QID, uint32, error) {
	return createViaMknod(vd, name, flags, permissions, uid, gid)
}

func (vd *valueDir) Mknod(name string, mode p9.FileMode,
	major, minor uint32, uid p9.UID, gid p9.GID,
) (p9.QID, error) {
	if name != listenerFileName {
		// TODO: add error message
		return p9.QID{}, perrors.EACCES
	}
	maddr, err := vd.assemble()
	if err != nil {
		return p9.QID{}, err
	}
	if uid, gid, err = mkPreamble(vd, listenerFileName, uid, gid); err != nil {
		return p9.QID{}, err
	}
	listener, err := vd.listen(maddr, mode)
	if err != nil {
		return p9.QID{}, err
	}
	var (
		closeOnce,
		unlinkOnce sync.Once
		unlinked     atomic.Bool
		netErr       error
		fileListener = &listenerCloser{
			Listener: listener,
			closeFn: func() error {
				closeOnce.Do(func() {
					unlinked.Store(true)
					netErr = listener.Close()
				})
				return netErr
			},
		}
	)
	qid, file, err := vd.newListenerFile(mode, uid, gid, fileListener)
	if err != nil {
		return p9.QID{}, errors.Join(err, listener.Close())
	}
	if err := vd.Link(file, name); err != nil {
		return p9.QID{}, errors.Join(err, listener.Close())
	}
	var (
		link             = file.linkSync
		unlinkerListener = &listenerCloser{
			Listener: listener,
			closeFn: func() error {
				unlinkOnce.Do(func() {
					if !unlinked.Load() {
						unlinkChildSync(link)
					}
				})
				return fileListener.closeFn()
			},
		}
	)
	if err := vd.emitter.emit(unlinkerListener); err != nil {
		return p9.QID{}, errors.Join(err, unlinkerListener.Close())
	}
	return qid, nil
}

func (vd *valueDir) listen(maddr multiaddr.Multiaddr, permissions p9.FileMode) (manet.Listener, error) {
	udsPath, err := maybeGetUDSPath(maddr)
	if err != nil {
		return nil, err
	}
	var cleanup func() error
	if len(udsPath) > 0 {
		hostPermissions := permissions.Permissions().OSMode()
		if cleanup, err = maybeMakeParentDir(udsPath, hostPermissions); err != nil {
			return nil, err
		}
	}
	listener, err := manet.Listen(maddr)
	if err != nil {
		if cleanup != nil {
			return nil, errors.Join(err, cleanup())
		}
		return nil, err
	}
	var (
		closeFn = func() error {
			err := listener.Close()
			if cleanup != nil {
				if cErr := cleanup(); cErr != nil {
					return errors.Join(err, cErr)
				}
			}
			return err
		}
		trackingListener = &connTracker{
			parent: vd,
			Listener: &listenerCloser{
				Listener: listener,
				closeFn:  closeFn,
			},
		}
	)
	return trackingListener, nil
}

func (vd *valueDir) newListenerFile(
	permissions p9.FileMode, uid p9.UID, gid p9.GID,
	listener manet.Listener,
) (p9.QID, *listenerFile, error) {
	var (
		path          = vd.path
		metadata, err = makeMetadata(p9.ModeRegular|permissions.Permissions(),
			WithUID[metadataOption](uid),
			WithGID[metadataOption](gid),
			WithPath[metadataOption](path),
		)
	)
	if err != nil {
		return p9.QID{}, nil, err
	}
	link, err := newLinkSync(
		WithParent[linkOption](vd, listenerFileName),
		WithoutRename[linkOption](true),
	)
	if err != nil {
		return p9.QID{}, nil, err
	}
	metadata.Size = uint64(len(listener.Multiaddr().String()))
	listenerFile := &listenerFile{
		metadata: metadata,
		linkSync: link,
		Listener: listener,
	}
	metadata.incrementPath()
	return *metadata.QID, listenerFile, nil
}

func (vd *valueDir) UnlinkAt(name string, flags uint32) error {
	directory := vd.directory
	_, file, err := directory.Walk([]string{name})
	if err != nil {
		return err
	}
	// NOTE: non-fs errors are ignored in this operation.
	if lFile, ok := file.(*listenerFile); ok {
		lFile.Listener.Close()
	}
	if _, ok := file.(*connDir); ok {
		// HACK: we can't compare this file
		// and our vd.*file (because our Walk
		// gives a unique instance). So we just
		// assume it's the one we constructed.
		// If we expect files to move around
		// a UUID could be placed on the connDir.
		// Or a deconstructor could be paired with it
		// (similar to how ephemeral dirs ref count works).
		vd.connDirMu.Lock()
		*vd.connDirPtr = nil
		vd.connDirMu.Unlock()
	}
	return errors.Join(
		file.Close(),
		directory.UnlinkAt(name, flags),
	)
}

func (pd *valueDir) assemble() (multiaddr.Multiaddr, error) {
	tail := pd.component
	if isPath := tail == nil; isPath {
		return pd.assemblePath()
	}
	var components []multiaddr.Multiaddr
	for current := pd.linkSync.parent; current != nil; {
		switch v := current.(type) {
		case *protocolDir:
			current = v.linkSync.parent
		case *valueDir:
			components = append(components, v.component)
			current = v.linkSync.parent
		default:
			current = nil
		}
	}
	reverse(components)
	return multiaddr.Join(append(components, tail)...), nil
}

func (vd *valueDir) assemblePath() (multiaddr.Multiaddr, error) {
	var (
		link    = vd.linkSync.link
		names   = []string{link.child}
		current = link.parent
	)
	for intermediate, ok := current.(*valueDir); ok; intermediate, ok = current.(*valueDir) {
		current = intermediate.linkSync.parent
		names = append(names, intermediate.linkSync.child)
	}
	protoDir, ok := current.(*protocolDir)
	if !ok {
		return nil, fmt.Errorf("%T is not a protocol directory", current)
	}
	reverse(names)
	var (
		prefix      = "/" + protoDir.protocol.Name
		components  = append([]string{prefix}, names...)
		maddrString = path.Join(components...)
	)
	return multiaddr.NewMultiaddr(maddrString)
}

func (vd *valueDir) getConnDir() (*connDir, error) {
	vd.connDirMu.Lock()
	defer vd.connDirMu.Unlock()
	if cd := *vd.connDirPtr; cd != nil {
		_, f, err := cd.Walk(nil)
		if err != nil {
			return nil, err
		}
		return f.(*connDir), nil
	}
	uid, gid, err := mkPreamble(vd, connectionsFileName, p9.NoUID, p9.NoGID)
	if err != nil {
		return nil, err
	}
	const permissions = ExecuteOther |
		ExecuteGroup | WriteGroup | ReadGroup |
		ExecuteUser | WriteUser | ReadUser
	cleanup := vd.cleanupEmpties
	_, dir, err := NewDirectory(
		WithPath[DirectoryOption](vd.path),
		WithPermissions[DirectoryOption](permissions),
		WithUID[DirectoryOption](uid),
		WithGID[DirectoryOption](gid),
		WithParent[DirectoryOption](vd, connectionsFileName),
		UnlinkWhenEmpty[DirectoryOption](cleanup),
		UnlinkEmptyChildren[DirectoryOption](cleanup),
	)
	if err != nil {
		return nil, err
	}
	cd := &connDir{
		directory: dir,
		path:      vd.path,
	}
	_, f, err := cd.Walk(nil)
	if err != nil {
		return nil, err
	}
	vd.connDirPtr = &cd
	vd.Link(cd, connectionsFileName)
	return f.(*connDir), nil
}

func (cd *connDir) Walk(names []string) ([]p9.QID, p9.File, error) {
	qids, file, err := cd.directory.Walk(names)
	if len(names) == 0 {
		file = &connDir{
			directory: file,
			path:      cd.path,
		}
	}
	return qids, file, err
}

func (cd *connDir) Link(file p9.File, name string) error {
	if _, ok := file.(*connFile); !ok {
		return fmt.Errorf("%w - unexpected file type", perrors.EACCES)
	}
	return cd.directory.Link(file, name)
}

func (cd *connDir) UnlinkAt(name string, flags uint32) error {
	directory := cd.directory
	_, file, err := directory.Walk([]string{name})
	if err != nil {
		return err
	}
	if cFile, ok := file.(*connFile); ok {
		cFile.trackedConn.Close()
	}
	return errors.Join(
		file.Close(),
		directory.UnlinkAt(name, flags),
	)
}

func (cd *connDir) newConnFile(name string, id uintptr, permissions p9.FileMode, uid p9.UID, gid p9.GID,
	conn trackedConn,
) (p9.QID, *connFile, error) {
	uid, gid, err := maybeInheritIDs(cd, uid, gid)
	if err != nil {
		return p9.QID{}, nil, err
	}
	path := cd.path
	metadata, err := makeMetadata(p9.ModeRegular|permissions,
		WithUID[metadataOption](uid),
		WithGID[metadataOption](gid),
		WithPath[metadataOption](path),
	)
	if err != nil {
		return p9.QID{}, nil, err
	}

	link, err := newLinkSync(
		WithParent[linkOption](cd, name),
		WithoutRename[linkOption](true),
	)
	if err != nil {
		return p9.QID{}, nil, err
	}
	connFile := &connFile{
		connID:      id,
		trackedConn: conn,
		metadata:    metadata,
		linkSync:    link,
	}
	metadata.incrementPath()
	return *metadata.QID, connFile, nil
}

func (lf *listenerFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) > 0 {
		return nil, nil, perrors.ENOTDIR
	}
	if lf.opened() {
		return nil, nil, fidOpenedErr
	}
	return nil, &listenerFile{
		Listener: lf.Listener,
		metadata: lf.metadata,
		linkSync: lf.linkSync,
	}, nil
}

func (lf *listenerFile) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	return lf.metadata.SetAttr(valid, attr)
}

func (lf *listenerFile) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return lf.metadata.GetAttr(req)
}

func (lf *listenerFile) Open(mode p9.OpenFlags) (p9.QID, ioUnit, error) {
	if lf.opened() {
		return p9.QID{}, 0, perrors.EBADF
	}
	lf.openFlags = lf.withOpenedFlag(mode)
	return *lf.QID, 0, nil
}

func (lf *listenerFile) Close() error {
	lf.openFlags = 0
	lf.ReaderAt = nil
	return nil
}

func (lf *listenerFile) ReadAt(p []byte, offset int64) (int, error) {
	reader := lf.ReaderAt
	if reader == nil {
		if !lf.canRead() {
			return -1, perrors.EBADF
		}
		data := lf.Listener.Multiaddr().String()
		reader = strings.NewReader(data)
		lf.ReaderAt = reader
	}
	return reader.ReadAt(p, offset)
}

func (lc *listenerCloser) Close() error { return lc.closeFn() }

func getProtocol(name string) (*multiaddr.Protocol, error) {
	protocol := multiaddr.ProtocolWithName(name)
	if protocol.Code == 0 {
		return nil, fmt.Errorf("\"%s\" not a valid protocol", name)
	}
	return &protocol, nil
}

// reverse is a generic adaption of gopls' `slice.reverse`.
// Named just for clarity.
func reverse[T any](slc []T) {
	for i, j := 0, len(slc)-1; i < j; i, j = i+1, j-1 {
		slc[i], slc[j] = slc[j], slc[i]
	}
}

// maybeGetUDSPath will return the first
// Unix Domain Socket path within maddr, if any.
// The returned path should be a suitable file path.
func maybeGetUDSPath(maddr multiaddr.Multiaddr) (string, error) {
	for _, protocol := range maddr.Protocols() {
		if protocol.Code == multiaddr.P_UNIX {
			udsPath, err := maddr.ValueForProtocol(protocol.Code)
			if err != nil {
				return "", err
			}
			if runtime.GOOS == "windows" {
				// `/C:\path` -> `C:\path`
				return strings.TrimPrefix(udsPath, `/`), nil
			}
			return udsPath, nil
		}
	}
	return "", nil
}

// maybeMakeParentDir may create a parent directory
// for path, if one does not exist. And `rmDir` will remove it.
// If path's parent does exist, `rmDir` will be nil.
func maybeMakeParentDir(path string, permissions fs.FileMode) (rmDir func() error, _ error) {
	socketDir := filepath.Dir(path)
	if err := os.Mkdir(socketDir, permissions); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil, nil
		}
		return nil, err
	}
	return func() error {
		return os.Remove(socketDir)
	}, nil
}

func splitMaddr(maddr multiaddr.Multiaddr) (components []*multiaddr.Component, names []string) {
	multiaddr.ForEach(maddr, func(c multiaddr.Component) bool {
		components = append(components, &c)
		names = append(names, strings.Split(c.String(), "/")[1:]...)
		return true
	})
	return
}

func (ct *connTracker) Accept() (manet.Conn, error) {
	conn, err := ct.Listener.Accept()
	if err != nil {
		return nil, err
	}
	parent := ct.parent
	connDir, err := parent.getConnDir()
	if err != nil {
		return nil, unwind(err, conn.Close)
	}
	var (
		closeOnce,
		unlinkOnce sync.Once
		unlinked atomic.Bool
		netErr   error
		tracked  = p9net.NewTrackedConn(conn)
		fileConn = &connCloser{
			trackedConn: tracked,
			closeFn: func() error {
				closeOnce.Do(func() {
					unlinked.Store(true)
					netErr = tracked.Close()
				})
				return netErr
			},
		}
		index = parent.connIndex.Add(1)
		name  = strconv.Itoa(int(index))
	)
	const permissions = ReadOther | ReadGroup | ReadUser
	_, file, err := connDir.newConnFile(
		name, index,
		permissions, p9.NoUID, p9.NoGID,
		fileConn,
	)
	if err != nil {
		return nil, unwind(err, conn.Close, connDir.Close)
	}
	if err := connDir.Link(file, name); err != nil {
		return nil, unwind(err, conn.Close, connDir.Close)
	}
	var (
		link         = file.linkSync
		connUnlinker = &connCloser{
			trackedConn: fileConn,
			closeFn: func() error {
				unlinkOnce.Do(func() {
					if !unlinked.Load() {
						unlinkChildSync(link)
					}
				})
				return fileConn.closeFn()
			},
		}
	)
	if err := connDir.Close(); err != nil {
		return nil, unwind(err, conn.Close, fileConn.Close)
	}
	return connUnlinker, nil
}

func (cf *connFile) marshal() ([]byte, error) {
	tracked := cf.trackedConn
	return json.Marshal(ConnInfo{
		ID:        cf.connID,
		Local:     tracked.LocalMultiaddr(),
		Remote:    tracked.RemoteMultiaddr(),
		LastRead:  tracked.LastRead(),
		LastWrite: tracked.LastWrite(),
	})
}

func (cf *connFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) > 0 {
		return nil, nil, perrors.ENOTDIR
	}
	if cf.opened() {
		return nil, nil, fidOpenedErr
	}
	return nil, &connFile{
		connID:      cf.connID,
		trackedConn: cf.trackedConn,
		metadata:    cf.metadata,
		linkSync:    cf.linkSync,
	}, nil
}

func (cf *connFile) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	return cf.metadata.SetAttr(valid, attr)
}

func (cf *connFile) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	if req.Size {
		data, err := cf.marshal()
		if err != nil {
			return p9.QID{}, p9.AttrMask{}, p9.Attr{}, err
		}
		cf.metadata.Size = uint64(len(data))
	}
	return cf.metadata.GetAttr(req)
}

func (cf *connFile) Open(mode p9.OpenFlags) (p9.QID, ioUnit, error) {
	if cf.opened() {
		return p9.QID{}, 0, perrors.EBADF
	}
	cf.openFlags = cf.withOpenedFlag(mode)
	return *cf.QID, 0, nil
}

func (cf *connFile) Close() error {
	cf.openFlags = 0
	cf.ReaderAt = nil
	return nil
}

func (cf *connFile) ReadAt(p []byte, offset int64) (int, error) {
	reader := cf.ReaderAt
	if reader == nil {
		if !cf.canRead() {
			return -1, perrors.EBADF
		}
		data, err := cf.marshal()
		if err != nil {
			return -1, err
		}
		reader = bytes.NewReader(data)
		cf.ReaderAt = reader
	}
	return reader.ReadAt(p, offset)
}

func (cc *connCloser) Close() error { return cc.closeFn() }

func (ci *ConnInfo) UnmarshalJSON(data []byte) error {
	var maddrBuff struct {
		Local  string `json:"local"`
		Remote string `json:"remote"`
	}
	if err := json.Unmarshal(data, &maddrBuff); err != nil {
		return err
	}
	var err error
	if ci.Local, err = multiaddr.NewMultiaddr(maddrBuff.Local); err != nil {
		return err
	}
	if ci.Remote, err = multiaddr.NewMultiaddr(maddrBuff.Remote); err != nil {
		return err
	}
	return json.Unmarshal(data, &struct {
		ID        *uintptr   `json:"#"`
		LastRead  *time.Time `json:"lastRead"`
		LastWrite *time.Time `json:"lastWrite"`
	}{
		ID:       &ci.ID,
		LastRead: &ci.LastRead, LastWrite: &ci.LastWrite,
	})
}
