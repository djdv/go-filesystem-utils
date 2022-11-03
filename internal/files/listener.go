package files

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	ListenerCallback = func(manet.Listener)
	Listener         struct {
		directory
		path           ninePath
		mknodCallback  ListenerCallback
		prefix         multiaddr.Multiaddr
		protocol       string
		maddrPath      []string
		cleanupEmpties bool
	}
	listenerDir interface {
		p9.File
		fileTable
	}
	listenerFile struct {
		templatefs.NoopFile
		metadata
		Listener manet.Listener
		// TODO: proper file I/O; methods are hacky right now
		// maddrReader *bytes.Reader
		openFlag
	}
	listenerUDSWrapper struct {
		manet.Listener
		closeFunc func() error
	}
)

func NewListener(callback ListenerCallback, options ...ListenerOption) (p9.QID, *Listener) {
	var settings listenerSettings
	if err := parseOptions(&settings, options...); err != nil {
		panic(err)
	}
	// TODO: rdev value?
	var (
		qid              p9.QID
		fsys             directory
		unlinkSelf       = settings.cleanupSelf
		directoryOptions = []DirectoryOption{
			WithSuboptions[DirectoryOption](settings.metaSettings.asOptions()...),
			WithSuboptions[DirectoryOption](settings.linkSettings.asOptions()...),
		}
	)
	if unlinkSelf {
		qid, fsys = newEphemeralDirectory(directoryOptions...)
	} else {
		qid, fsys = NewDirectory(directoryOptions...)
	}
	return qid, &Listener{
		path:           settings.ninePath,
		directory:      fsys,
		mknodCallback:  callback,
		cleanupEmpties: settings.cleanupElements,
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
	typedDir, err := assertDirectory(dirClone)
	if err != nil {
		return nil, nil, err
	}
	newDir := &Listener{
		directory:     typedDir,
		path:          ld.path,
		mknodCallback: ld.mknodCallback,
		prefix:        ld.prefix,
		protocol:      ld.protocol,
	}
	return qids, newDir, nil
}

func (ld *Listener) Walk(names []string) ([]p9.QID, p9.File, error) {
	return walk[*Listener](ld, names...)
}

func (ld *Listener) fidOpened() bool { return false } // TODO need to store state or read &.dir's

func (ld *Listener) files() fileTable {
	// XXX: Magic; We need to change something to eliminate this.
	return ld.directory.(interface {
		files() fileTable
	}).files()
}

func (ld *Listener) Mkdir(name string, permissions p9.FileMode, _ p9.UID, gid p9.GID) (p9.QID, error) {
	var (
		prefix       = ld.prefix
		protocolName = ld.protocol
		maddrPath    = ld.maddrPath
	)
	// TODO: try to make this less gross / split up.
	if protocolName == "" {
		if err := validateProtocol(name); err != nil {
			// TODO: error value
			return p9.QID{}, fmt.Errorf("%w - %s", perrors.EIO, err)
		}
		protocolName = name
	} else {
		// TODO: [1413c980-2a65-4144-a679-7be1b77f01e3] generalize.
		// multiaddr pkg should have an index with a .Path flag set
		// for ones that need this behaviour. Right now hardcoding UDS support only.
		if protocolName == "unix" {
			maddrPath = append(maddrPath, name)
		} else {
			var err error
			if prefix, err = appendMaddr(prefix, protocolName, name); err != nil {
				// TODO: error value
				return p9.QID{}, fmt.Errorf("%w - %s", perrors.EIO, err)
			}
			protocolName = ""
		}
	}
	attr, err := mkdirInherit(ld, permissions, gid)
	if err != nil {
		return p9.QID{}, err
	}
	var (
		qid              p9.QID
		fsys             directory
		directoryOptions = []DirectoryOption{
			WithSuboptions[DirectoryOption](
				WithPath(ld.path),
				WithBaseAttr(attr),
				WithAttrTimestamps(true),
			),
			WithSuboptions[DirectoryOption](
				WithParent(ld, name),
			),
		}
	)
	if ld.cleanupEmpties {
		qid, fsys = newEphemeralDirectory(directoryOptions...)
	} else {
		qid, fsys = NewDirectory(directoryOptions...)
	}
	// TODO: Maybe (re)add internal options and call the constructor here
	// (instead of using a literal).
	// withPrefix, withProtocol, ...
	newDir := &Listener{
		path:           ld.path,
		directory:      fsys,
		mknodCallback:  ld.mknodCallback,
		prefix:         prefix,
		protocol:       protocolName,
		maddrPath:      maddrPath,
		cleanupEmpties: ld.cleanupEmpties,
	}
	return qid, ld.Link(newDir, name)
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

	protocol := ld.protocol
	// TODO: [1413c980-2a65-4144-a679-7be1b77f01e3]
	if protocol == "unix" {
		name = path.Join(append(ld.maddrPath, name)...)
	}
	component, err := multiaddr.NewComponent(protocol, name)
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
	attr, err := mknodInherit(ld, mode, gid)
	if err != nil {
		return p9.QID{}, err
	}
	listenerFile := ld.makeListenerFile(listener, name, attr)
	return *listenerFile.QID, ld.Link(listenerFile, name)
}

func (ld *Listener) makeListenerFile(listener manet.Listener,
	name string, attr *p9.Attr,
) *listenerFile {
	attr.Size = uint64(len(listener.Multiaddr().Bytes()))
	listenerFile := &listenerFile{
		metadata: metadata{
			ninePath: ld.path,
			Attr:     attr,
		},
		Listener: listener,
	}
	const withTimestamps = true
	initMetadata(&listenerFile.metadata, p9.ModeRegular, withTimestamps)
	return listenerFile
}

func (ld *Listener) Listen(maddr multiaddr.Multiaddr) (manet.Listener, error) {
	var (
		_, names   = splitMaddr(maddr)
		components = names[:len(names)-1]
		socket     = names[len(names)-1]
		want       = p9.AttrMask{
			Mode: true,
			UID:  true,
			GID:  true,
		}
		required  = p9.AttrMask{Mode: true}
		attr, err = maybeGetAttrs(ld.directory, want, required)
	)
	if err != nil {
		return nil, err
	}

	var (
		basePermissions = attr.Mode.Permissions()
		dirPermissions  = mkdirMask(basePermissions)
		sockPermissions = socketMask(basePermissions)
		uid             = attr.UID
		gid             = attr.GID
	)
	protocolDir, err := MkdirAll(ld, components, dirPermissions, uid, gid)
	if err != nil {
		return nil, err
	}

	// TODO: Close the listener in the event of an FS/link err?
	listener, err := listen(maddr)
	if err != nil {
		return nil, err
	}

	attr.Mode = sockPermissions
	listenerFile := ld.makeListenerFile(listener, socket, attr)
	return listener, protocolDir.Link(listenerFile, socket)
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
			// maddrReader: lf.maddrReader,
		}
	)
	if withQID {
		qids = []p9.QID{*newLf.QID}
	}
	return qids, newLf, nil
}

func (lf *listenerFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	return walk[*listenerFile](lf, names...)
}

func (lf *listenerFile) Open(mode p9.OpenFlags) (p9.QID, ioUnit, error) {
	if lf.fidOpened() {
		return p9.QID{}, 0, perrors.EBADF
	}
	lf.openFlag = true
	if mode.Mode() != p9.ReadOnly {
		// TODO: [spec] correct evalue?
		return p9.QID{}, 0, perrors.EINVAL
	}
	// TODO: this but properly, cache the reader and whatnot.
	// lf.maddrReader = bytes.NewReader(lf.Listener.Multiaddr().Bytes())
	return *lf.QID, 0, nil
}

func (lf *listenerFile) ReadAt(p []byte, offset int64) (int, error) {
	if !lf.fidOpened() { // TODO: spec compliance check - may need to check flags too.
		return 0, perrors.EINVAL
	}
	// TODO: properly cache the reader here.
	// return lf.maddrReader.ReadAt(p, offset)
	return bytes.NewReader(lf.Listener.Multiaddr().Bytes()).ReadAt(p, offset)
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
