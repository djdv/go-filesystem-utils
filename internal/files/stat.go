package files

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/hugelgupf/p9/p9"
)

type ioUnit = uint32

const (
	noIOUnit ioUnit = 0

	// Permission mode bits.
	//
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

	// Plan 9 - Open(5) Create masks.

	// S_REGMSK = S_IRWXA &^ (S_IXUSR | S_IXGRP | S_IXOTH)
	// S_DIRMSK = S_IRWXA

	// TODO: where used, should be variable. With this only being the default.
	// umask must be configurable at runtime, at least at the root level.

	// Linux - Open(2) umask.

	S_LINMSK = S_IWGRP | S_IWOTH
)

type (
	metadata struct {
		path ninePath
		*p9.Attr
		*p9.QID
	}

	openFlag bool

	unixTime struct {
		seconds, nanoseconds *uint64
	}
)

func (b openFlag) fidOpened() bool { return bool(b) }

func (md metadata) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	var (
		ourAtime   = !valid.ATimeNotSystemTime
		ourMtime   = !valid.MTimeNotSystemTime
		cTime      = valid.CTime
		usingClock = ourAtime || ourMtime || cTime
	)
	if usingClock {
		for _, time := range []struct {
			name               string
			useClock, setField bool
		}{
			{useClock: ourAtime, setField: valid.ATime, name: "A"},
			{useClock: ourMtime, setField: valid.MTime, name: "M"},
		} {
			if time.useClock && !time.setField {
				const (
					namePrefix = "P9_SETATTR_"
					nameSuffix = "TIME"
				)
				return fmt.Errorf(
					"system time requested, but corresponding time field (%s) was not set",
					namePrefix+time.name+nameSuffix,
				)
			}
		}
		timestamp(timePointers(md.Attr, ourAtime, ourMtime, cTime))
		for _, b := range []struct {
			timeFlag  *bool
			clockFlag bool
		}{
			{
				timeFlag:  &valid.ATime,
				clockFlag: ourAtime,
			},
			{
				timeFlag:  &valid.MTime,
				clockFlag: ourMtime,
			},
		} {
			if b.clockFlag {
				*b.timeFlag = false
			}
		}
	}
	md.Apply(valid, attr)
	return nil
}

func (md metadata) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	var (
		qid          = *md.QID
		filled, attr = fillAttrs(req, md.Attr)
	)
	return qid, filled, *attr, nil
}

func initMetadata(metadata *metadata, fileType p9.FileMode, withTimestamps bool) {
	attr := metadata.Attr
	if attr == nil {
		attr = &p9.Attr{
			UID: p9.NoUID,
			GID: p9.NoGID,
		}
		metadata.Attr = attr
	}
	attr.Mode |= fileType
	if withTimestamps {
		timestamp(timePointers(attr, true, true, true))
	}
	path := metadata.path
	if path == nil {
		path = new(atomic.Uint64)
		metadata.path = path
	}
	var (
		pathNum = path.Add(1)
		qidType = fileType.QIDType()
	)
	metadata.QID = &p9.QID{
		Type: qidType,
		Path: pathNum,
	}
}

func timePointers(attr *p9.Attr, A, M, C bool) []unixTime {
	var fields int
	for _, b := range []bool{A, M, C} {
		if b {
			fields++
		}
	}
	times := make([]unixTime, 0, fields)
	for _, t := range []struct {
		seconds, nanoseconds *uint64
		setFlag              bool
	}{
		{
			setFlag:     A,
			seconds:     &attr.ATimeSeconds,
			nanoseconds: &attr.ATimeNanoSeconds,
		},
		{
			setFlag:     M,
			seconds:     &attr.MTimeSeconds,
			nanoseconds: &attr.MTimeNanoSeconds,
		},
		{
			setFlag:     C,
			seconds:     &attr.CTimeSeconds,
			nanoseconds: &attr.CTimeNanoSeconds,
		},
	} {
		if t.setFlag {
			times = append(times, unixTime{
				seconds:     t.seconds,
				nanoseconds: t.nanoseconds,
			})
		}
	}
	return times
}

func timestamp(times []unixTime) {
	var (
		now     = time.Now()
		nowSec  = uint64(now.Unix())
		nowNano = uint64(now.UnixNano())
	)
	for _, field := range times {
		*field.seconds, *field.nanoseconds = nowSec, nowNano
	}
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

func setAttr(file p9.File, attr *p9.Attr, withServerTimes bool) error {
	valid, setAttr := attrToSetAttr(attr)
	if withServerTimes {
		valid.ATime = true
		valid.MTime = true
		valid.CTime = true
	}
	return file.SetAttr(valid, setAttr)
}

func getAttrs(file p9.File, want p9.AttrMask) (*p9.Attr, error) {
	_, valid, attr, err := file.GetAttr(want)
	if err != nil {
		return nil, err
	}
	if !valid.Contains(want) {
		return nil, attrErr(valid, want)
	}
	return &attr, nil
}

func maybeGetAttrs(file p9.File, want, required p9.AttrMask) (*p9.Attr, error) {
	_, valid, attr, err := file.GetAttr(want)
	if err != nil {
		return nil, err
	}
	if !valid.Contains(required) {
		return nil, attrErr(valid, want)
	}
	if want.UID && !valid.UID {
		attr.UID = p9.NoUID
	}
	if want.GID && !valid.GID {
		attr.GID = p9.NoGID
	}
	return &attr, nil
}

func attrErr(got, want p9.AttrMask) error {
	return fmt.Errorf("did not receive expected attributes"+
		"\n\tgot: %s"+
		"\n\twant: %s",
		got, want,
	)
}

/* [lint] Seems worse than the if-wall. Maybe can be reworked?
for _ field := range []struct {
	requested, isValid bool
	rValid *bool
	value ,	rValue any
} {
	{
		requested: req.Size,
		isValid: !attr.IsDir(),
		value: attr.Size,
		rValid: &valid.Size,
		rValue: &rAttr.Size,
	}
}{
	fillAttr(field.requested,
		field.isValid, field.value,
		field.rValue, field.rValue,
	)
}

fillAttr(req.Size,
	true, attr.Size,
	&valid.Size, &rAttr.Size,
)
func fillAttr[T any](requested, isValid bool, value T, rValid *bool, rValue *T,
) {
	if requested && isValid() {
	*rValue, *rValid = value, true
}
}
*/

func attrToSetAttr(source *p9.Attr) (p9.SetAttrMask, p9.SetAttr) {
	var (
		valid p9.SetAttrMask
		attr  p9.SetAttr
		uid   = source.UID
		gid   = source.GID
	)
	if permissions := source.Mode.Permissions(); permissions != 0 {
		attr.Permissions, valid.Permissions = permissions, true
	}
	attr.UID, valid.UID = uid, uid.Ok()
	attr.GID, valid.GID = gid, gid.Ok()
	if size := source.Size; size != 0 {
		attr.Size, valid.Size = size, true
	}
	for _, timeAttr := range []struct {
		setTime, localTime *bool
		value              uint64
	}{
		{
			value:     source.ATimeNanoSeconds,
			setTime:   &valid.ATime,
			localTime: &valid.ATimeNotSystemTime,
		},
		{
			value:     source.MTimeNanoSeconds,
			setTime:   &valid.MTime,
			localTime: &valid.MTimeNotSystemTime,
		},
	} {
		if timeAttr.value != 0 {
			*timeAttr.setTime = true
			*timeAttr.localTime = true
		}
	}
	return valid, attr
}

func mkdirMask(permissions p9.FileMode) p9.FileMode { return (permissions &^ S_LINMSK) & S_IRWXA }
func mknodMask(permissions p9.FileMode) p9.FileMode { return permissions &^ S_LINMSK }

func maybeInheritUID(parent file) (*p9.Attr, error) {
	var (
		want     = p9.AttrMask{UID: true}
		required = p9.AttrMask{}
	)
	return maybeGetAttrs(parent, want, required)
}

// TODO: better name. mkdirFillAttr?
// TODO: 9P2000.L does not define UID as part of mkdir messages.
// The library/fork we're using should probably remove it from the method interface.
func mkdirInherit(parent file, permissions p9.FileMode, gid p9.GID) (*p9.Attr, error) {
	attr, err := maybeInheritUID(parent)
	if err != nil {
		return nil, err
	}
	return &p9.Attr{
		Mode: mkdirMask(permissions),
		UID:  attr.UID,
		GID:  gid,
	}, nil
}

// TODO: 9P2000.L does not define UID as part of mknod messages.
// The library/fork we're using should probably remove it from the method interface.
func mknodInherit(parent file, permissions p9.FileMode, gid p9.GID) (*p9.Attr, error) {
	attr, err := maybeInheritUID(parent)
	if err != nil {
		return nil, err
	}
	return &p9.Attr{
		Mode: mknodMask(permissions),
		UID:  attr.UID,
		GID:  gid,
	}, nil
}
