package daemon

import (
	"fmt"
	"io"
	"sync/atomic"

	"github.com/djdv/go-filesystem-utils/internal/motd"
	"github.com/djdv/go-filesystem-utils/internal/p9p/errors"
	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
)

type (
	devClass    = uint32
	devInstance = uint32
)

const apiDev devClass = iota

const (
	shutdownInst devInstance = iota
	motdInst
)

const (
	motdName     = "MOTD"
	shutdownName = "shutdown"
)

// TODO:
// We need file/path serial numbers+tables
// Create initializes a file,
// atomically increments a number,
// and associates it with this file.
// The string path -> number,
// and number -> file must be stored by the system.
// Open shall look for these.
// ^ devices will need to be assigned id numbers themselves
// so that both may be combined
// otherwise 2 devices could return
// file id 1 for their first file
type (
	// TODO: better name; Server?
	Root struct {
		p9.QID
		p9.Attr
		templatefs.NoopFile
		// socket   net.Listener
		socket   io.Closer // TODO: better name? socketCloser?
		Shutdown bool      // TODO: better name? gracefulShutdown? shutdownInvoked? ??
		// ^ this should probably be exposed (only) as a method

		// NOTE: For simplicity, the prototype file system hierarchy
		// works in single layers only.
		// Real implementations can use a real tree.
		fileTable map[string]p9.File
		path      *atomic.Uint64
	}
)

// TODO: better names
func NewRoot(listener io.Closer) *Root {
	const (
		deviceCountHint = 2         // MOTD, Shutdown
		placeholderDev  = p9.Dev(0) // TODO from opts?
	)
	root := &Root{
		QID: p9.QID{
			Type:    p9.TypeDir,
			Version: 1, // TODO: we can use this as the API version; or treat it for real (modify it when devices are added)
			Path:    0, // TODO: different value; maybe a constant 1 or a real incremented one?
		},
		Attr: p9.Attr{
			Mode: p9.ModeDirectory,
			UID:  p9.NoUID,
			GID:  p9.NoGID,
			RDev: placeholderDev,
		},
		socket:    listener,
		fileTable: make(map[string]p9.File, deviceCountHint),
		path:      new(atomic.Uint64),
	}
	if err := setupRootDevices(root); err != nil {
		panic(err)
	}
	return root
}

func setupRootDevices(root *Root) error {
	for _, pair := range []struct {
		devMode  p9.FileMode
		instance devInstance
	}{
		{p9.ModeBlockDevice, shutdownInst},
		{p9.ModeCharacterDevice, motdInst},
	} {
		if _, err := root.Mknod(motdName, pair.devMode, apiDev, pair.instance, 0, 0); err != nil {
			return err
		}
	}
	return nil
}

func (r *Root) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	var (
		qid    = r.QID
		attr   p9.Attr
		filled p9.AttrMask
	)
	if req.Empty() {
		return qid, filled, attr, nil
	}

	if req.Mode {
		attr.Mode, filled.Mode = r.Attr.Mode, true
	}
	if req.UID {
		attr.UID, filled.UID = r.Attr.UID, true
	}
	if req.GID {
		attr.GID, filled.GID = r.Attr.GID, true
	}
	if req.GID {
		attr.GID, filled.GID = r.Attr.GID, true
	}
	if req.RDev {
		attr.RDev, filled.RDev = r.Attr.RDev, true
	}

	return qid, filled, attr, nil
}

func (root *Root) Attach() (p9.File, error) {
	return root, nil
}

func (r *Root) Mknod(name string, mode p9.FileMode,
	major uint32, minor uint32,
	uid p9.UID, gid p9.GID,
) (p9.QID, error) {
	if !mode.IsBlockDevice() &&
		!mode.IsCharacterDevice() {
		return p9.QID{}, fmt.Errorf(`mode "%v" does not specify a device`, mode)
	}
	switch major {
	case apiDev:
		return r.makeDevice(name, minor)
	default:
		return p9.QID{}, fmt.Errorf("bad device-class type: %d want %d", major, apiDev) // TODO: err format
	}
}

func (r *Root) makeDevice(name string, instanceType devInstance) (p9.QID, error) {
	switch instanceType {
	case motdInst:
		if _, exists := r.fileTable[name]; exists {
			return p9.QID{}, errors.EEXIST // TODO: double check spec - EEXIST is probably right
		}
		motdDir, qid := motd.NewMOTD([]string{name}, r.path)
		r.fileTable[name] = motdDir
		return qid, nil
	case shutdownInst:
		return p9.QID{}, nil // FIXME: stubbed for testing
		// return p9.QID{}, goerrors.New("NIY")
		// return id.Get(p9.TypeTemporary), nil
	default:
		return p9.QID{}, fmt.Errorf("bad device-instance type: %d want %d|%d",
			instanceType, motdInst, shutdownInst) // TODO: err format
	}
}

func (r *Root) Walk(names []string) (qids []p9.QID, f p9.File, err error) {
	switch nameCount := len(names); nameCount {
	case 0:
		nr := new(Root)
		*nr = *r
		return nil, nr, nil
	case 1:
		if names[0] == "." {
			nr := new(Root)
			*nr = *r
			return []p9.QID{nr.QID}, nr, nil
		}
		var (
			name       = names[0]
			device, ok = r.fileTable[name]
		)
		if !ok {
			return nil, nil, errors.ENOENT
		}
		qid, _, _, err := device.GetAttr(p9.AttrMask{})
		if err != nil {
			return nil, nil, err
		}
		return []p9.QID{qid}, device, nil
	default:
		return nil, nil, fmt.Errorf("dir: depth max is 1 for now")
	}
}
