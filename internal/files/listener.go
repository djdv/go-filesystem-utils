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
		mknodCallback ListenerCallback
		prefix        multiaddr.Multiaddr
		*Directory
		protocol string

		// TODO: stopped *atomic.Bool
		// when true, mkdir+mknod returns an error; no callbacks are called.
		// ^ also descends (recursivley) on file tables, closing all socket files.
		// ^^ also need callback in unlink
		// sets intentional close flag/bool/whatever
		// so server handle loop can distinguish if
		// shutdown-error value was expected or not.
	}
	listenerFile struct {
		templatefs.NoopFile
		parentFile  p9.File
		Listener    manet.Listener
		maddrReader *bytes.Reader
		path        *atomic.Uint64
		*p9.Attr
		*p9.QID
	}
	listenerUDSWrapper struct {
		manet.Listener
		closeFunc func() error
	}
)

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

func NewListener(options ...ListenerOption) *Listener {
	var (
		qid, attr = newMeta(p9.TypeDir)
		listener  = &Listener{
			Directory: &Directory{
				QID:     qid,
				Attr:    attr,
				entries: newFileTable(),
			},
		}
	)
	for _, setFunc := range options {
		if err := setFunc(listener); err != nil {
			panic(err)
		}
	}
	setupOrUsePather(&listener.QID.Path, &listener.path)
	return listener
}

func newListenerDir(callback ListenerCallback, prefix multiaddr.Multiaddr, protocol string,
	options ...DirectoryOption,
) *Listener {
	return &Listener{
		mknodCallback: callback,
		prefix:        prefix,
		protocol:      protocol,
		Directory:     NewDirectory(options...),
	}
}

func (ld *Listener) clone(withQID bool) ([]p9.QID, *Listener) {
	qids, dirClone := ld.Directory.clone(withQID)
	newDir := &Listener{
		mknodCallback: ld.mknodCallback,
		prefix:        ld.prefix,
		Directory:     dirClone,
		protocol:      ld.protocol,
	}
	return qids, newDir
}

func (ld *Listener) Walk(names []string) ([]p9.QID, p9.File, error) {
	return walk[*Listener](ld, names...)
}

func (ld *Listener) Mkdir(name string, permissions p9.FileMode, _ p9.UID, gid p9.GID) (p9.QID, error) {
	if _, exists := ld.entries.load(name); exists {
		return p9.QID{}, perrors.EEXIST
	}
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
		// TODO: callback / channel / emitter needs a way to associate the listener
		// with the [Unlink] call, that that `rm sock` can somehow signal the server
		// "listener X was shutdown intentionally" i.e. disregard server-closed errors.
		// ^ simple atomic bool like we had before,
		// but pass to to callback and use it during unlink
		//
		// TODO: [4da77693-f66f-4384-9629-8dd79cd52d40] Wrap the closer sent to the callback
		// so it will unlink itself here, on Close in caller.
		// Counterpart to calling `rm sock` from OS rather than Go.
		callback = ld.mknodCallback
		newDir   = NewListener(
			WithParent[ListenerOption](ld),
			WithCallback(callback),
			withPrefix(prefix),
			withProtocol(protocolName),
		)
		uid = ld.UID
	)
	if err := newDir.SetAttr(mkdirMask(permissions, uid, gid)); err != nil {
		return *newDir.QID, err
	}
	return *newDir.QID, ld.Link(newDir, name)
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

	if _, exists := ld.entries.load(name); exists {
		return p9.QID{}, perrors.EEXIST
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

	uid := ld.UID
	return createListenerFile(listener, name,
		mode, ld,
		ld.path, uid, gid)
}

func (ld *Listener) Listen(maddr multiaddr.Multiaddr) (manet.Listener, error) {
	var (
		_, names    = splitMaddr(maddr)
		components  = names[:len(names)-1]
		socket      = names[len(names)-1]
		permissions = ld.Mode.Permissions()
		uid         = ld.UID
		gid         = ld.GID
	)
	protocolDir, err := MkdirAll(ld, components, permissions, uid, gid)
	if err != nil {
		return nil, err
	}

	listener, err := listen(maddr)
	if err != nil {
		return nil, err
	}

	if _, err := createListenerFile(listener, socket,
		permissions, protocolDir,
		ld.path, uid, gid); err != nil {
		return nil, err
	}

	return listener, nil
}

func createListenerFile(listener manet.Listener, name string, permissions p9.FileMode,
	parent p9.File, path *atomic.Uint64,
	uid p9.UID, gid p9.GID,
) (p9.QID, error) {
	listenerFile := makeListenerFile(listener,
		WithParent[listenerOption](parent),
		WithPath[listenerOption](path),
	)
	if err := listenerFile.SetAttr(mknodMask(permissions, uid, gid)); err != nil {
		return *listenerFile.QID, err
	}
	if err := parent.Link(listenerFile, name); err != nil {
		return *listenerFile.QID, err
	}
	return *listenerFile.QID, nil
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
	// TODO: can we generalise the file table so it returns concrete types?
	// ^ should we?
	lf := ld.entries.pop(name)
	if lf == nil {
		return perrors.ENOENT
	}
	if lFile, ok := lf.(*listenerFile); ok {
		if listener := lFile.Listener; listener != nil {
			err := listener.Close()
			return err
		}
	}
	return nil
}

func makeListenerFile(listener manet.Listener, options ...listenerOption,
) *listenerFile {
	lf := &listenerFile{
		Listener: listener,
		QID:      &p9.QID{Type: p9.TypeRegular},
		Attr: &p9.Attr{
			Mode: p9.ModeRegular,
			UID:  p9.NoUID,
			GID:  p9.NoGID,
			Size: uint64(len(listener.Multiaddr().Bytes())),
		},
	}
	for _, setFunc := range options {
		if err := setFunc(lf); err != nil {
			panic(err)
		}
	}
	setupOrUsePather(&lf.QID.Path, &lf.path)
	return lf
}

func (lf *listenerFile) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	lf.Attr.Apply(valid, attr)
	return nil
}

func (lf *listenerFile) clone(withQID bool) ([]p9.QID, *listenerFile) {
	var (
		qids  []p9.QID
		newLf = &listenerFile{
			QID:        lf.QID,
			Attr:       lf.Attr,
			parentFile: lf.parentFile,
			path:       lf.path,
			Listener:   lf.Listener,
			// unlinkErr:  lf.unlinkErr,
			// Closed:     lf.Closed,
		}
	)
	if withQID {
		qids = []p9.QID{*newLf.QID}
	}
	return qids, newLf
}

func (lf *listenerFile) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	var (
		qid          = *lf.QID
		filled, attr = fillAttrs(req, lf.Attr)
	)
	return qid, filled, *attr, nil
}

func (lf *listenerFile) fidOpened() bool  { return lf.maddrReader != nil }
func (lf *listenerFile) files() fileTable { return nil }
func (lf *listenerFile) parent() p9.File  { return lf.parentFile }

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
