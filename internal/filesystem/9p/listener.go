package p9

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
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
		protocol      *multiaddr.Protocol
		renameAllowed *atomic.Bool
	}
	valueDir struct {
		directory
		*linkSync
		*listenerShared
		component     *multiaddr.Component
		renameAllowed *atomic.Bool
	}
	listenerFile struct {
		templatefs.NoopFile
		unlinked *atomic.Bool
		metadata
		Listener manet.Listener
		*linkSync
		io.ReaderAt
	}
	listenerOnceCloser struct {
		manet.Listener
		error
		sync.Once
	}
	listenerCloser struct {
		manet.Listener
		afterCloseFn func() error
		unlinked     *atomic.Bool
	}
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
	const channelBuffer = 0
	var (
		emitter   = makeChannelEmitter[manet.Listener](ctx, channelBuffer)
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
		_, names   = splitMaddr(maddr)
		components = names[:len(names)-1]
		socket     = names[len(names)-1]
		uid        = p9.NoUID
		gid        = p9.NoGID
	)
	protocolDir, err := MkdirAll(listener, components, permissions, uid, gid)
	if err != nil {
		return err
	}
	_, err = protocolDir.Mknod(socket, permissions, 0, 0, p9.NoUID, p9.NoGID)
	if cErr := protocolDir.Close(); cErr != nil {
		err = fserrors.Join(err, cErr)
	}
	return err
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
	if _, err := getProtocol(name); err != nil {
		return fmt.Errorf("%w - %s", perrors.EIO, err)
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
		renameAllowed:  new(atomic.Bool),
	}
	return qid, ld.directory.Link(protoDir, name)
}

func (ls *listenerShared) listen(maddr multiaddr.Multiaddr, permissions p9.FileMode) (manet.Listener, error) {
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
			return nil, fserrors.Join(err, cleanup())
		}
		return nil, err
	}
	if cleanup != nil {
		listener = &listenerCloser{
			Listener:     listener,
			afterCloseFn: cleanup,
		}
	}
	return &listenerOnceCloser{Listener: listener}, nil
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

func (ls *listenerShared) mkListenerFile(parent p9.File, name string,
	permissions p9.FileMode, uid p9.UID, gid p9.GID,
	listener manet.Listener,
) (p9.QID, *listenerFile, error) {
	var (
		path          = ls.path
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
		WithParent[linkOption](parent, name),
		WithoutRename[linkOption](true),
	)
	if err != nil {
		return p9.QID{}, nil, err
	}
	metadata.Size = uint64(len(listener.Multiaddr().String()))
	listenerFile := &listenerFile{
		metadata: metadata,
		unlinked: new(atomic.Bool),
		linkSync: link,
		Listener: listener,
	}
	metadata.incrementPath()
	return *metadata.QID, listenerFile, nil
}

func (ls *listenerShared) rename(directory p9.File, oldName, newName string) error {
	_, oldFile, err := directory.Walk([]string{oldName})
	if err != nil {
		return err
	}
	_, _, attr, err := oldFile.GetAttr(p9.AttrMask{Mode: true})
	if err != nil {
		return err
	}
	if _, err := directory.Mknod(newName, attr.Mode, 0, 0, p9.NoUID, p9.NoGID); err != nil {
		return err
	}
	const flags = 0
	if err := ls.unlinkAt(directory, oldName, flags); err != nil {
		return err
	}
	return oldFile.Close()
}

func (ls *listenerShared) mknod(parent p9.File, maddr multiaddr.Multiaddr,
	name string, mode p9.FileMode, uid p9.UID, gid p9.GID,
) (p9.QID, error) {
	uid, gid, err := mkPreamble(parent, name, uid, gid)
	if err != nil {
		return p9.QID{}, err
	}
	listener, err := ls.listen(maddr, mode)
	if err != nil {
		return p9.QID{}, err
	}
	qid, file, err := ls.mkListenerFile(parent, name, mode, uid, gid, listener)
	if err != nil {
		return p9.QID{}, err
	}
	if err := parent.Link(file, name); err != nil {
		return p9.QID{}, fserrors.Join(err, listener.Close())
	}
	fileListener := file.unlinkOnListenerClose()
	if err := ls.emitter.emit(fileListener); err != nil {
		return p9.QID{}, fserrors.Join(err, fileListener.Close())
	}
	return qid, nil
}

// unlinkAt will always unlink name,
// but if its file contains a listener, it will also close the listener.
func (ls *listenerShared) unlinkAt(parent directory, name string, flags uint32) error {
	_, file, wErr := parent.Walk([]string{name})
	if wErr != nil {
		return wErr
	}
	err := parent.UnlinkAt(name, flags)
	if lFile, ok := file.(*listenerFile); ok {
		lFile.unlinked.Store(true)
		listener := lFile.Listener
		err = fserrors.Join(err, listener.Close())
	}
	return fserrors.Join(err, file.Close())
}

func (pd *protocolDir) Walk(names []string) ([]p9.QID, p9.File, error) {
	qids, file, err := pd.directory.Walk(names)
	if len(names) == 0 {
		file = &protocolDir{
			directory:      file,
			listenerShared: pd.listenerShared,
			linkSync:       pd.linkSync,
			protocol:       pd.protocol,
			renameAllowed:  pd.renameAllowed,
		}
	}
	return qids, file, err
}

func (pd *protocolDir) RenameAt(oldName string, newDir p9.File, newName string) error {
	if !pd.renameAllowed.Load() {
		return fmt.Errorf("%w - only directories containing listeners may rename", perrors.EACCES)
	}
	clone, ok := newDir.(*protocolDir)
	if !ok || clone.protocol != pd.protocol {
		return fmt.Errorf("%w - only direct descendants may be moved", perrors.EACCES)
	}
	return pd.listenerShared.rename(pd, oldName, newName)
}

func (pd *protocolDir) Renamed(newDir p9.File, newName string) {
	pd.directory.Renamed(newDir, newName)
}

func (pd *protocolDir) Link(file p9.File, name string) error {
	if !pd.protocol.Path {
		protocol := pd.protocol.Name
		if _, err := multiaddr.NewComponent(protocol, name); err != nil {
			return err
		}
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
		renameAllowed:  new(atomic.Bool),
	}
	return qid, pd.directory.Link(newDir, name)
}

func (pd *protocolDir) Create(name string, flags p9.OpenFlags,
	permissions p9.FileMode, uid p9.UID, gid p9.GID,
) (p9.File, p9.QID, uint32, error) {
	return createViaMknod(pd, name, flags, permissions, uid, gid)
}

func (pd *protocolDir) Mknod(name string, mode p9.FileMode,
	major, minor uint32, uid p9.UID, gid p9.GID,
) (p9.QID, error) {
	maddr, err := pd.assemble(name)
	if err != nil {
		return p9.QID{}, err
	}
	qid, err := pd.mknod(pd, maddr, name, mode, uid, gid)
	if err == nil {
		pd.renameAllowed.Store(true)
	}
	return qid, err
}

func (pd *protocolDir) UnlinkAt(name string, flags uint32) error {
	return pd.unlinkAt(pd.directory, name, flags)
}

func (pd *protocolDir) assemble(name string) (multiaddr.Multiaddr, error) {
	tail, err := multiaddr.NewComponent(pd.protocol.Name, name)
	if err != nil {
		return nil, err
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

func (vd *valueDir) Walk(names []string) ([]p9.QID, p9.File, error) {
	qids, file, err := vd.directory.Walk(names)
	if len(names) == 0 {
		file = &valueDir{
			directory:      file,
			listenerShared: vd.listenerShared,
			linkSync:       vd.linkSync,
			component:      vd.component,
			renameAllowed:  vd.renameAllowed,
		}
	}
	return qids, file, err
}

func (vd *valueDir) RenameAt(oldName string, newDir p9.File, newName string) error {
	if !vd.renameAllowed.Load() {
		return fmt.Errorf("%w - only directories containing listeners may rename", perrors.EACCES)
	}
	clone, ok := newDir.(*valueDir)
	if !ok || clone.component != vd.component {
		return fmt.Errorf("%w - only direct descendants may be moved", perrors.EACCES)
	}
	return vd.listenerShared.rename(vd, oldName, newName)
}

func (vd *valueDir) Renamed(newDir p9.File, newName string) {
	vd.linkSync.Renamed(newDir, newName)
}

func (vd *valueDir) Link(file p9.File, name string) error {
	if isPathType := vd.component == nil; !isPathType {
		if _, err := getProtocol(name); err != nil {
			return fmt.Errorf("%w - %s", perrors.EIO, err)
		}
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
			listenerShared: vd.listenerShared,
			linkSync:       link,
			directory:      directory,
			renameAllowed:  new(atomic.Bool),
		}
		return qid, vd.directory.Link(valueDir, name)
	}
	protocol, err := getProtocol(name)
	if err != nil {
		// TODO: error value
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
		renameAllowed:  new(atomic.Bool),
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
	maddr, err := vd.assemble(name)
	if err != nil {
		return p9.QID{}, err
	}
	qid, err := vd.mknod(vd, maddr, name, mode, uid, gid)
	if err == nil {
		vd.renameAllowed.Store(true)
	}
	return qid, err
}

func (vd *valueDir) UnlinkAt(name string, flags uint32) error {
	return vd.unlinkAt(vd.directory, name, flags)
}

func (vd *valueDir) assemble(name string) (multiaddr.Multiaddr, error) {
	var (
		names   = []string{name, vd.linkSync.child}
		current = vd.linkSync.parent
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

func (lf *listenerFile) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	return lf.metadata.SetAttr(valid, attr)
}

func (lf *listenerFile) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return lf.metadata.GetAttr(req)
}

func (lf *listenerFile) opened() bool {
	return lf.ReaderAt != nil
}

func (lf *listenerFile) unlinkOnListenerClose() manet.Listener {
	var (
		link     = lf.linkSync
		unlinked = lf.unlinked
	)
	return &listenerCloser{
		Listener: lf.Listener,
		unlinked: unlinked,
		afterCloseFn: func() error {
			link.mu.Lock()
			defer link.mu.Unlock()
			if unlinked.Load() {
				return nil
			}
			unlinked.Store(true)
			const flags = 0
			return link.parent.UnlinkAt(link.child, flags)
		},
	}
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
		unlinked: lf.unlinked,
		linkSync: lf.linkSync,
	}, nil
}

func (lf *listenerFile) Rename(newDir p9.File, newName string) error {
	return lf.linkSync.rename(lf, newDir, newName)
}

func (lf *listenerFile) Renamed(newDir p9.File, newName string) {
	lf.linkSync.Renamed(newDir, newName)
}

func (lf *listenerFile) Open(mode p9.OpenFlags) (p9.QID, ioUnit, error) {
	if lf.opened() {
		return p9.QID{}, 0, perrors.EBADF
	}
	// TODO: expose binary mode.
	// Either here via a flag, or in ReadAt via negative offset.
	// ^control file might be less brittle.
	lf.ReaderAt = strings.NewReader(lf.Listener.Multiaddr().String())
	// lf.ReaderAt = bytes.NewReader(lf.Listener.Multiaddr().Bytes())
	return *lf.QID, 0, nil
}

func (lf *listenerFile) ReadAt(p []byte, offset int64) (int, error) {
	if !lf.opened() { // TODO: spec compliance check - may need to check flags too.
		return 0, perrors.EINVAL
	}
	return lf.ReaderAt.ReadAt(p, offset)
}

func (lc *listenerOnceCloser) Close() error {
	lc.Once.Do(func() { lc.error = lc.Listener.Close() })
	return lc.error
}

func (lc *listenerCloser) Close() error {
	return fserrors.Join(lc.Listener.Close(), lc.afterCloseFn())
}

func (lc *listenerCloser) Unlinked() bool {
	return lc.unlinked.Load()
}

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
