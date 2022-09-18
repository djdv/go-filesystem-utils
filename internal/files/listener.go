package files

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	ListenerCallback = func(manet.Listener)
	Listener         struct {
		templatefs.NoopFile

		// uid  p9.UID
		path *atomic.Uint64

		// path *atomic.Uint64
		// directory *Directory // FIXME: this needs to become an interface?
		// or a generic type [Directory|EphemeralDir]
		// ^^ can this just be [fileTable]?
		directory     p9.File
		mknodCallback ListenerCallback
		prefix        multiaddr.Multiaddr
		protocol      string

		// TODO: stopped *atomic.Bool
		// when true, mkdir+mknod returns an error; no callbacks are called.
		// ^ also descends (recursivley) on file tables, closing all socket files.
		// ^^ also need callback in unlink
		// sets intentional close flag/bool/whatever
		// so server handle loop can distinguish if
		// shutdown-error value was expected or not.
	}
	listenerDir interface {
		p9.File
		fileTable
	}
	listenerFile struct {
		templatefs.NoopFile
		metadata
		Listener    manet.Listener
		maddrReader *bytes.Reader
	}
	listenerUDSWrapper struct {
		manet.Listener
		closeFunc func() error
	}
)

func (ld *Listener) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	return ld.directory.SetAttr(valid, attr)
}

func (ld *Listener) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return ld.directory.GetAttr(req)
}

// TODO: this has to be changed? Require the callbacks for mknod and unlink?
// accept metadata options.
func NewListener(callback ListenerCallback, options ...MetaOption) *Listener {
	_, root := NewDirectory(options...)
	/*
		return &Listener{
			path:          listeners.path,
			directory:     listeners,
			mknodCallback: callback,
		}
	*/
	return newListener(callback, root, root.path)
}

func newListener(callback ListenerCallback, directory p9.File, path *atomic.Uint64) *Listener {
	return &Listener{
		path:          path,
		directory:     directory,
		mknodCallback: callback,
	}
}

func (ld *Listener) fidOpened() bool { return false } // TODO need to store state or read &.dir's
func (ld *Listener) files() fileTable {
	// XXX: We need to change something to eliminate this switch.
	switch t := ld.directory.(type) {
	case *Directory:
		return t.fileTable
	case *ephemeralDir:
		return t.fileTable
	default:
		return nil
	}
}

func (ld *Listener) clone(withQID bool) ([]p9.QID, *Listener, error) {
	var wnames []string
	if withQID {
		wnames = []string{selfWName}
	}
	qids, dirClone, err := ld.directory.Walk(wnames)
	if err != nil {
		return nil, nil, err
	}
	newDir := &Listener{
		mknodCallback: ld.mknodCallback,
		prefix:        ld.prefix,
		directory:     dirClone,
		protocol:      ld.protocol,
	}
	return qids, newDir, nil
}

func (ld *Listener) Walk(names []string) ([]p9.QID, p9.File, error) {
	return walk[*Listener](ld, names...)
}

func (ld *Listener) Mkdir(name string, permissions p9.FileMode, _ p9.UID, gid p9.GID) (p9.QID, error) {
	var (
		prefix       = ld.prefix
		protocolName = ld.protocol
	)
	if protocolName == "" {
		if err := validateProtocol(name); err != nil {
			// TODO: error value
			return p9.QID{}, fmt.Errorf("%w - %s", perrors.EIO, err)
		}
		protocolName = name
	} else {
		var err error
		if prefix, err = appendMaddr(prefix, protocolName, name); err != nil {
			// TODO: error value
			return p9.QID{}, fmt.Errorf("%w - %s", perrors.EIO, err)
		}
		protocolName = ""
	}
	var (
		want                = p9.AttrMask{UID: true}
		_, valid, attr, err = ld.directory.GetAttr(want)

		qid, eDir = newEphemeralDir(ld, name, WithPath(ld.path))
		newDir    = &Listener{
			path:          eDir.path,
			directory:     eDir,
			mknodCallback: ld.mknodCallback,
			prefix:        prefix,
			protocol:      protocolName,
		}

		validSet, setAttr = attrToSetAttr(&p9.Attr{
			Mode: (permissions.Permissions() &^ S_LINMSK) & S_IRWXA,
			UID:  attr.UID,
			GID:  gid,
		})
	)
	validSet.ATime = true
	validSet.MTime = true
	validSet.CTime = true
	if err != nil {
		return p9.QID{}, err
	}
	if !valid.Contains(want) {
		return p9.QID{}, attrErr(valid, want)
	}
	if err := newDir.SetAttr(validSet, setAttr); err != nil {
		return qid, err
	}
	return qid, ld.directory.Link(newDir, name)
}

func validateProtocol(name string) error {
	protocol := multiaddr.ProtocolWithName(name)
	if protocol.Code == 0 {
		return fmt.Errorf("\"%s\" not a valid protocol", name)
	}
	return nil
}

func appendMaddr(prefix multiaddr.Multiaddr, protocol, value string) (multiaddr.Multiaddr, error) {
	component, err := multiaddr.NewComponent(protocol, value)
	if err != nil {
		return nil, err
	}
	if prefix != nil {
		return prefix.Encapsulate(component), nil
	}
	return component, nil
}

func (ld *Listener) Create(name string, flags p9.OpenFlags,
	permissions p9.FileMode, uid p9.UID, gid p9.GID,
) (p9.File, p9.QID, uint32, error) {
	if qid, err := ld.Mknod(name, permissions|p9.ModeRegular, 0, 0, uid, gid); err != nil {
		return nil, qid, 0, err
	}
	_, lf, err := ld.Walk([]string{name})
	if err != nil {
		return nil, p9.QID{}, 0, err
	}
	qid, n, err := lf.Open(flags)
	if err != nil {
		return nil, p9.QID{}, 0, err
	}
	return lf, qid, n, nil
}

func (ld *Listener) Mknod(name string, mode p9.FileMode,
	major uint32, minor uint32, _ p9.UID, gid p9.GID,
) (p9.QID, error) {
	callback := ld.mknodCallback
	if callback == nil {
		return p9.QID{}, perrors.ENOSYS
	}

	component, err := multiaddr.NewComponent(ld.protocol, name)
	if err != nil {
		return p9.QID{}, err
	}
	var maddr multiaddr.Multiaddr = component
	if prefix := ld.prefix; prefix != nil {
		maddr = prefix.Encapsulate(component)
	}

	listener, err := listen(maddr)
	if err != nil {
		return p9.QID{}, err
	}
	defer callback(listener)
	// TODO: we need to close the listener,
	// in case of file system error before return.
	// (Extending the defer to check nrErr, would probably be easiest.)

	want := p9.AttrMask{UID: true}
	_, valid, attr, err := ld.directory.GetAttr(want)
	if err != nil {
		return p9.QID{}, err
	}
	if valid.Contains(want) {
		return p9.QID{}, attrErr(valid, want)
	}
	permissions := mode.Permissions() &^ S_LINMSK
	/*
		return createListenerFile(listener, name,
			permissions, ld, ld.path, attr.UID, gid)
	*/
	listenerFile, err := makeListenerFile(listener,
		permissions, attr.UID, gid, WithPath(ld.path))
	qid := *listenerFile.QID
	if err != nil {
		return qid, err
	}
	return qid, ld.Link(listenerFile, name)
}

func (ld *Listener) Listen(maddr multiaddr.Multiaddr) (manet.Listener, error) {
	var (
		listener, err = listen(maddr)
		_, names      = splitMaddr(maddr)
		components    = names[:len(names)-1]
		socket        = names[len(names)-1]
		want          = p9.AttrMask{
			Mode: true,
			UID:  true,
			GID:  true,
		}
	)
	if err != nil {
		return nil, err
	}

	// TODO: Close the listener in the event of an FS err

	_, valid, attr, err := ld.directory.GetAttr(want)
	if err != nil {
		return nil, err
	}
	if !valid.Contains(want) {
		return nil, attrErr(valid, want)
	}
	var (
		permissions  = attr.Mode.Permissions()
		permissionsD = (permissions &^ S_LINMSK) & S_IRWXA
		permissionsF = permissions &^ S_LINMSK
		uid          = attr.UID
		gid          = attr.GID
	)
	protocolDir, err := MkdirAll(ld, components, permissionsD, uid, gid)
	if err != nil {
		return nil, err
	}
	/*
		if _, err := createListenerFile(listener, socket,
			permissionsF, protocolDir,
			ld.path, uid, gid); err != nil {
			return nil, err
		}

		return listener, nil
	*/
	listenerFile, err := makeListenerFile(listener,
		permissionsF, uid, gid, WithPath(ld.path))
	if err != nil {
		return nil, err
	}
	return nil, protocolDir.Link(listenerFile, socket)
}

// TODO: split listen up into 3 phases
// listen; mkfile; linkfile. Listen and Mknod call all 3,
// but only mknod inserts a callback between the last phase.
func listen(maddr multiaddr.Multiaddr) (manet.Listener, error) {
	var (
		err     error
		cleanup func() error
	)
	for _, protocol := range maddr.Protocols() {
		if protocol.Code == multiaddr.P_UNIX {
			var udsPath string
			if udsPath, err = maddr.ValueForProtocol(protocol.Code); err != nil {
				break
			}
			if runtime.GOOS == "windows" { // `/C:\path` -> `C:\path`
				udsPath = strings.TrimPrefix(udsPath, `/`)
			}
			socketDir := filepath.Dir(udsPath)

			// TODO: permission check
			const permissions = 0o775
			if err = os.Mkdir(socketDir, permissions); err != nil {
				break
			}
			cleanup = func() error {
				return os.Remove(socketDir)
			}
			break
		}
	}
	if err != nil {
		return nil, err
	}
	listener, err := manet.Listen(maddr)
	if err != nil {
		if cleanup != nil {
			if cErr := cleanup(); cErr != nil {
				return nil, cErr
			}
		}
		return nil, err
	}
	if cleanup != nil {
		return &listenerUDSWrapper{
			Listener:  listener,
			closeFunc: cleanup,
		}, nil
	}
	return listener, nil
}

func createListenerFile(listener manet.Listener, name string, permissions p9.FileMode,
	parent p9.File, path *atomic.Uint64,
	uid p9.UID, gid p9.GID,
) (p9.QID, error) {
	var (
		listenerFile, err = makeListenerFile(listener,
			permissions, uid, gid, WithPath(path))
		qid = *listenerFile.QID
	)
	if err != nil {
		return qid, err
	}
	return qid, parent.Link(listenerFile, name)
}

func makeListenerFile(listener manet.Listener,
	permissions p9.FileMode, uid p9.UID, gid p9.GID,
	options ...MetaOption,
) (*listenerFile, error) {
	var (
		meta         = makeMetadata(p9.ModeRegular, options...)
		listenerFile = &listenerFile{
			Listener: listener,
			metadata: meta,
		}
		validSet, setAttr = attrToSetAttr(&p9.Attr{
			Mode: permissions,
			UID:  uid,
			GID:  gid,
			Size: uint64(len(listener.Multiaddr().Bytes())),
		})
	)
	return listenerFile, listenerFile.SetAttr(validSet, setAttr)
}

func splitMaddr(maddr multiaddr.Multiaddr) (components []*multiaddr.Component, names []string) {
	multiaddr.ForEach(maddr, func(c multiaddr.Component) bool {
		components = append(components, &c)
		names = append(names, strings.Split(c.String(), "/")[1:]...)
		return true
	})
	return
}

// getFirstUnixSocketPath returns the path
// of the first Unix domain socket within the multiaddr (if any)
func getFirstUnixSocketPath(ma multiaddr.Multiaddr) (target string) {
	multiaddr.ForEach(ma, func(comp multiaddr.Component) bool {
		isUnixComponent := comp.Protocol().Code == multiaddr.P_UNIX
		if isUnixComponent {
			target = comp.Value()
			if runtime.GOOS == "windows" { // `/C:\path` -> `C:\path`
				target = strings.TrimPrefix(target, `/`)
			}
			return true
		}
		return false
	})
	return
}

func (ld *Listener) UnlinkAt(name string, flags uint32) error {
	_, lFile, err := ld.Walk([]string{name})
	if err != nil {
		return err
	}

	// TODO: we should do an internal [Open] with flags [ORCLOSE]
	// then unlink from our table,
	// then return the result of file.Close (which itself calls listener.Close)

	ulErr := ld.directory.UnlinkAt(name, flags)
	if lf, ok := lFile.(*listenerFile); ok {
		if listener := lf.Listener; listener != nil {
			if err := listener.Close(); err != nil {
				return err
			}
		}
	}
	return ulErr
}

func (lf *listenerFile) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	return lf.metadata.SetAttr(valid, attr)
}

func (lf *listenerFile) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return lf.metadata.GetAttr(req)
}

func (lf *listenerFile) clone(withQID bool) ([]p9.QID, *listenerFile, error) {
	var (
		qids  []p9.QID
		newLf = &listenerFile{
			metadata: lf.metadata,
			Listener: lf.Listener,
		}
	)
	if withQID {
		qids = []p9.QID{*newLf.QID}
	}
	return qids, newLf, nil
}

func (lf *listenerFile) fidOpened() bool { return lf.maddrReader != nil }

func (lf *listenerFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	return walk[*listenerFile](lf, names...)
}

func (lf *listenerFile) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	if lf.fidOpened() {
		return p9.QID{}, 0, perrors.EBADF
	}
	if mode.Mode() != p9.ReadOnly {
		// TODO: [spec] correct evalue?
		return p9.QID{}, 0, perrors.EINVAL
	}
	lf.maddrReader = bytes.NewReader(lf.Listener.Multiaddr().Bytes())
	return *lf.QID, 0, nil
}

func (lf *listenerFile) ReadAt(p []byte, offset int64) (int, error) {
	if !lf.fidOpened() { // TODO: spec compliance check - may need to check flags too.
		return 0, perrors.EINVAL
	}
	return lf.maddrReader.ReadAt(p, offset)
}

func (udl *listenerUDSWrapper) Close() error {
	var (
		lErr = udl.Listener.Close()
		cErr = udl.closeFunc()
	)
	if cErr == nil {
		return lErr
	}
	return cErr
}
