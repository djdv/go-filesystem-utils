package files

import (
	"sync/atomic"
	"time"

	"github.com/hugelgupf/p9/p9"
)

// Permission mode bits.
const (
	// POSIX.

	S_IROTH p9.FileMode = p9.Read
	S_IWOTH             = p9.Write
	S_IXOTH             = p9.Exec

	i_modeShift = 3

	S_IRGRP = S_IROTH << i_modeShift
	S_IWGRP = S_IWOTH << i_modeShift
	S_IXGRP = S_IXOTH << i_modeShift

	S_IRUSR = S_IRGRP << i_modeShift
	S_IWUSR = S_IWGRP << i_modeShift
	S_IXUSR = S_IXGRP << i_modeShift

	S_IRWXO = S_IROTH | S_IWOTH | S_IXOTH
	S_IRWXG = S_IRGRP | S_IWGRP | S_IXGRP
	S_IRWXU = S_IRUSR | S_IWUSR | S_IXUSR

	// Non-standard.

	S_IRWXA = S_IRWXU | S_IRWXG | S_IRWXO              // 0777
	S_IRXA  = S_IRWXA &^ (S_IWUSR | S_IWGRP | S_IWOTH) // 0555

	// TODO: operation masks should be configurable during node creation?
	// Currently operations are hardcoded to use Linux umask(2) style.

	// Open(5) Create masks.

	// S_REGMSK = S_IRWXA &^ (S_IXUSR | S_IXGRP | S_IXOTH)
	// S_DIRMSK = S_IRWXA

	// TODO: where used, should be variable. With this only being the default.
	// umask must be configurable at runtime, at least at the root level.

	// Linux umask

	S_LINMSK = S_IWGRP | S_IWOTH
)

type metadata struct {
	// parentFile p9.File
	path *atomic.Uint64
	*p9.Attr
	*p9.QID
}

func makeMetadata(filetype p9.FileMode, options ...MetaOption) metadata {
	data := metadata{
		QID: &p9.QID{Type: filetype.QIDType()},
		Attr: &p9.Attr{
			Mode: filetype.FileType(),
			UID:  p9.NoUID,
			GID:  p9.NoGID,
		},
	}
	for _, setFunc := range options {
		if err := setFunc(&data); err != nil {
			panic(err)
		}
	}
	setupOrUsePather(&(data.QID.Path), &(data.path))
	return data
}

// func (md metadata) parent() p9.File { return md.parentFile }
func (md metadata) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	var (
		ourAtime   = !valid.ATimeNotSystemTime
		ourMtime   = !valid.MTimeNotSystemTime
		cTime      = valid.CTime
		usingClock = ourAtime || ourMtime || cTime
	)
	if usingClock {
		var (
			now     = time.Now()
			nowSec  = uint64(now.Second())
			nowNano = uint64(now.Nanosecond())
		)
		for _, x := range []struct {
			set  bool
			flag *bool
			s, n *uint64
		}{
			{
				set:  ourAtime,
				flag: &valid.ATime,
				s:    &attr.ATimeSeconds,
				n:    &attr.ATimeNanoSeconds,
			},
			{
				set:  ourMtime,
				flag: &valid.MTime,
				s:    &attr.MTimeSeconds,
				n:    &attr.MTimeNanoSeconds,
			},
			{
				set:  cTime,
				flag: &valid.CTime,
				s:    &md.Attr.CTimeSeconds,
				n:    &md.Attr.CTimeNanoSeconds,
			},
		} {
			if x.set {
				*x.s, *x.n = nowSec, nowNano
				*x.flag = false
			}
		}
	}
	md.Attr.Apply(valid, attr)
	return nil
}

func (md metadata) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	var (
		qid          = *md.QID
		filled, attr = fillAttrs(req, md.Attr)
	)
	return qid, filled, *attr, nil
}

// If the optional parent pather was provided, use it
// otherwise, make a new one and use that.
func setupOrUsePather(path *uint64, patherPtr **atomic.Uint64) {
	pather := *patherPtr
	if pather == nil {
		pather = new(atomic.Uint64)
		*patherPtr = pather
	}
	*path = pather.Add(1)
}

func fillAttrs(req p9.AttrMask, attr *p9.Attr) (p9.AttrMask, *p9.Attr) {
	var (
		rAttr  p9.Attr
		filled p9.AttrMask
	)
	if req.Empty() {
		return filled, &rAttr
	}

	if req.Mode {
		rAttr.Mode, filled.Mode = attr.Mode, true
	}
	if req.UID {
		rAttr.UID, filled.UID = attr.UID, true
	}
	if req.GID {
		rAttr.GID, filled.GID = attr.GID, true
	}
	if req.RDev {
		rAttr.RDev, filled.RDev = attr.RDev, true
	}
	if req.Size {
		rAttr.Size, filled.Size = attr.Size, true
	}

	return filled, &rAttr
}

// FIXME: currently assumes perm, uid, and gid are set
// should inspect source for dynamic mask.
func attrToSetAttr(source *p9.Attr) (p9.SetAttrMask, p9.SetAttr) {
	return p9.SetAttrMask{
			Permissions: true,
			UID:         true,
			GID:         true,
			ATime:       true,
			MTime:       true,
			CTime:       true,
		}, p9.SetAttr{
			Permissions: source.Mode,
			UID:         source.UID,
			GID:         source.GID,
		}
}

func mkdirMask(permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.SetAttrMask, p9.SetAttr) {
	return attrToSetAttr(&p9.Attr{
		Mode: (permissions &^ S_LINMSK) & S_IRWXA,
		UID:  uid,
		GID:  gid,
	})
}

func mknodMask(permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.SetAttrMask, p9.SetAttr) {
	return attrToSetAttr(&p9.Attr{
		Mode: permissions.Permissions() &^ S_LINMSK,
		UID:  uid,
		GID:  gid,
	})
}
