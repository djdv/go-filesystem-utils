package p9

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/p9/p9"
)

// Permission mode bits.
const (
	ExecuteOther p9.FileMode = p9.Exec << iota
	WriteOther
	ReadOther

	ExecuteGroup
	WriteGroup
	ReadGroup

	ExecuteUser
	WriteUser
	ReadUser
)

const noIOUnit ioUnit = 0

type (
	ioUnit   = uint32
	ninePath = *atomic.Uint64
	metadata struct {
		ninePath
		p9.Attr
		p9.QID
	}
	fileSettings struct {
		linkSync
		metadata
	}
	metadataSetter[T any] interface {
		*T
		setPath(ninePath)
		setPermissions(p9.FileMode)
		setUID(p9.UID)
		setGID(p9.GID)
	}
)

func (md *metadata) setPath(path ninePath) { md.ninePath = path }
func (md *metadata) setUID(uid p9.UID)     { md.UID = uid }
func (md *metadata) setGID(gid p9.GID)     { md.GID = gid }
func (md *metadata) setPermissions(permissions p9.FileMode) {
	md.Mode = md.Mode.FileType() |
		permissions.Permissions()
}

func (md *metadata) initialize(mode p9.FileMode) {
	var (
		now       = time.Now()
		sec, nano = uint64(now.Unix()), uint64(now.UnixNano())
	)
	md.Attr = p9.Attr{
		Mode: mode,
		UID:  p9.NoUID, GID: p9.NoGID,
		ATimeSeconds: sec, ATimeNanoSeconds: nano,
		MTimeSeconds: sec, MTimeNanoSeconds: nano,
		CTimeSeconds: sec, CTimeNanoSeconds: nano,
	}
	md.QID = p9.QID{
		Type: mode.QIDType(),
	}
}

func (md *metadata) fillDefaults() {
	if md.ninePath == nil {
		md.ninePath = new(atomic.Uint64)
	}
}

// WithPath specifies the path
// to be used by this file.
func WithPath[
	OT generic.OptionFunc[T],
	T any,
	I metadataSetter[T],
](path *atomic.Uint64,
) OT {
	return func(status *T) error {
		if path == nil {
			return generic.ConstError("path option's value is `nil`")
		}
		any(status).(I).setPath(path)
		return nil
	}
}

// WithPermissions specifies the permission bits
// for a file's mode status.
func WithPermissions[
	OT generic.OptionFunc[T],
	T any,
	I metadataSetter[T],
](permissions p9.FileMode,
) OT {
	return func(status *T) error {
		any(status).(I).setPermissions(permissions)
		return nil
	}
}

// WithUID specifies a UID value for
// a file's status information.
func WithUID[
	OT generic.OptionFunc[T],
	T any,
	I metadataSetter[T],
](uid p9.UID,
) OT {
	return func(status *T) error {
		any(status).(I).setUID(uid)
		return nil
	}
}

// WithGID specifies a GID value for
// a file's status information.
func WithGID[
	OT generic.OptionFunc[T],
	T any,
	I metadataSetter[T],
](gid p9.GID,
) OT {
	return func(status *T) error {
		any(status).(I).setGID(gid)
		return nil
	}
}

func (md *metadata) incrementPath() {
	md.QID.Path = md.ninePath.Add(1)
}

func (md *metadata) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	var (
		ourAttr  = md.Attr
		ourAtime = !valid.ATimeNotSystemTime
		ourMtime = !valid.MTimeNotSystemTime
		cTime    = valid.CTime
	)
	if usingClock := ourAtime || ourMtime || cTime; usingClock {
		var (
			now  = time.Now()
			sec  = uint64(now.Unix())
			nano = uint64(now.UnixNano())
		)
		if ourAtime {
			valid.ATime = false
			ourAttr.ATimeSeconds, ourAttr.ATimeNanoSeconds = sec, nano
		}
		if ourMtime {
			valid.MTime = false
			ourAttr.MTimeSeconds, ourAttr.MTimeNanoSeconds = sec, nano
		}
		if cTime {
			ourAttr.CTimeSeconds, ourAttr.CTimeNanoSeconds = sec, nano
		}
	}
	ourAttr.Apply(valid, attr)
	return nil
}

func (md *metadata) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	validAttrs(&req, &md.Attr)
	if req.INo {
		req.INo = md.ninePath != nil
	}
	return md.QID, req, md.Attr, nil
}

func validAttrs(req *p9.AttrMask, attr *p9.Attr) {
	if req.Empty() {
		return
	}
	if req.Mode {
		req.Mode = attr.Mode != 0
	}
	if req.NLink {
		req.NLink = attr.NLink != 0
	}
	if req.UID {
		req.UID = attr.UID.Ok()
	}
	if req.GID {
		req.GID = attr.GID.Ok()
	}
	if req.RDev {
		req.RDev = attr.RDev != 0
	}
	if req.ATime {
		req.ATime = attr.ATimeNanoSeconds != 0
	}
	if req.MTime {
		req.MTime = attr.MTimeNanoSeconds != 0
	}
	if req.CTime {
		req.CTime = attr.CTimeNanoSeconds != 0
	}
	if req.Size {
		req.Size = !attr.Mode.IsDir()
	}
	if req.Blocks {
		req.Blocks = attr.Blocks != 0
	}
	if req.BTime {
		req.BTime = attr.BTimeNanoSeconds != 0
	}
	if req.Gen {
		req.Gen = attr.Gen != 0
	}
	if req.DataVersion {
		req.DataVersion = attr.DataVersion != 0
	}
}

func attrErr(got, want p9.AttrMask) error {
	return fmt.Errorf("did not receive expected attributes"+
		"\n\tgot: %s"+
		"\n\twant: %s",
		got, want,
	)
}
