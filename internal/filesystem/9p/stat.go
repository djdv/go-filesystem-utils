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
	metadata struct { // TODO: R/W guard or atomic operations.
		ninePath
		*p9.Attr
		*p9.QID
	}
	metadataOption func(*metadata) error
)

func makeMetadata(mode p9.FileMode, options ...metadataOption) (metadata, error) {
	var (
		now       = time.Now()
		sec, nano = uint64(now.Unix()), uint64(now.UnixNano())
		meta      = metadata{
			ninePath: new(atomic.Uint64),
			Attr: &p9.Attr{
				Mode: mode,
				UID:  p9.NoUID, GID: p9.NoGID,
				ATimeSeconds: sec, ATimeNanoSeconds: nano,
				MTimeSeconds: sec, MTimeNanoSeconds: nano,
				CTimeSeconds: sec, CTimeNanoSeconds: nano,
			},
			QID: &p9.QID{
				Type: mode.QIDType(),
			},
		}
	)
	if err := parseOptions(&meta, options...); err != nil {
		return metadata{}, err
	}
	if meta.ninePath == nil {
		return metadata{}, generic.ConstError("[path] option's value is `nil`")
	}
	return meta, nil
}

func WithPath[OT Options](path *atomic.Uint64) (option OT) {
	return makeFieldSetter[OT]("ninePath", path)
}

func WithPermissions[OT Options](permissions p9.FileMode) (option OT) {
	return makeFieldFunc[OT]("Mode", func(mode *p9.FileMode) error {
		*mode = mode.FileType() | permissions.Permissions()
		return nil
	})
}

func WithUID[OT Options](uid p9.UID) (option OT) {
	return makeFieldSetter[OT]("UID", uid)
}

func WithGID[OT Options](gid p9.GID) (option OT) {
	return makeFieldSetter[OT]("GID", gid)
}

func (md metadata) incrementPath() {
	md.QID.Path = md.ninePath.Add(1)
}

func (md metadata) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
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

func (md metadata) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	var (
		qid          = *md.QID
		filled, attr = fillAttrs(req, md.Attr)
	)
	return qid, filled, *attr, nil
}

func fillAttrs(req p9.AttrMask, attr *p9.Attr) (p9.AttrMask, *p9.Attr) {
	var (
		rAttr p9.Attr
		valid p9.AttrMask
	)
	if req.Empty() {
		return valid, &rAttr
	}
	if req.Mode {
		mode := attr.Mode
		rAttr.Mode, valid.Mode = mode, mode != 0
	}
	if req.UID {
		uid := attr.UID
		rAttr.UID, valid.UID = uid, uid.Ok()
	}
	if req.GID {
		gid := attr.GID
		rAttr.GID, valid.GID = gid, gid.Ok()
	}
	if req.RDev {
		rDev := attr.RDev
		rAttr.RDev, valid.RDev = rDev, rDev != 0
	}
	if req.ATime {
		var (
			sec  = attr.ATimeSeconds
			nano = attr.ATimeNanoSeconds
		)
		rAttr.ATimeSeconds, rAttr.ATimeNanoSeconds, valid.ATime = sec, nano, nano != 0
	}
	if req.MTime {
		var (
			sec  = attr.MTimeSeconds
			nano = attr.MTimeNanoSeconds
		)
		rAttr.MTimeSeconds, rAttr.MTimeNanoSeconds, valid.MTime = sec, nano, nano != 0
	}
	if req.CTime {
		var (
			sec  = attr.CTimeSeconds
			nano = attr.CTimeNanoSeconds
		)
		rAttr.CTimeSeconds, rAttr.CTimeNanoSeconds, valid.CTime = sec, nano, nano != 0
	}
	if req.Size {
		rAttr.Size, valid.Size = attr.Size, !attr.Mode.IsDir()
	}
	return valid, &rAttr
}

func attrErr(got, want p9.AttrMask) error {
	return fmt.Errorf("did not receive expected attributes"+
		"\n\tgot: %s"+
		"\n\twant: %s",
		got, want,
	)
}
