package p9

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	perrors "github.com/djdv/p9/errors"
	"github.com/djdv/p9/fsimpl/templatefs"
	"github.com/djdv/p9/p9"
)

type (
	// FieldParser should parse and assign its inputs.
	// Returning either a [FieldError] if the key is not applicable,
	// or any other error if the value is invalid.
	FieldParser interface {
		ParseField(key, value string) error
	}
	// FieldError describes which key was searched for
	// and the available fields which were tried.
	// Useful for chaining [FieldParser.ParseField] calls with [errors.As].
	FieldError struct {
		Key   string
		Tried []string
	}
	SystemMaker interface {
		MakeFS() (fs.FS, error)
	}
	Mounter interface {
		Mount(fs.FS) (io.Closer, error)
	}
	mountPointTag struct {
		filesystem.Host `json:"host"`
		filesystem.ID   `json:"guest"`
	}
	mountPointMarshal struct {
		mountPointTag `json:"tag"`
		Data          json.RawMessage `json:"data"`
	}
	HostIdentifier interface {
		HostID() filesystem.Host
	}
	GuestIdentifier interface {
		GuestID() filesystem.ID
	}
	MountPoint interface {
		FieldParser
		SystemMaker
		Mounter
		HostIdentifier
		GuestIdentifier
	}
	MountPointFile[MP MountPoint] struct {
		mountPointFile
		mountPoint MP
		mountPointHost
		mountPointIO
	}
	mountPointFile struct {
		templatefs.NoopFile
		metadata
		mu       *sync.Mutex
		linkSync *linkSync
	}
	mountPointIO struct {
		reader *bytes.Reader
		buffer *bytes.Buffer
		openFlags
		fieldMode bool
		modified  bool
	}
	detachFunc     = func() error
	mountPointHost struct {
		unmountFn *detachFunc
	}
	mountPointSettings struct {
		fileOptions
	}
	MountPointOption func(*mountPointSettings) error
)

func (fe FieldError) Error() string {
	// Format:
	// unexpected key: "${key}", want one of: $QuotedCSV(${tried})
	const (
		delimiter  = ','
		space      = ' '
		separator  = string(delimiter) + string(space)
		separated  = len(separator)
		surrounder = '"'
		surrounded = len(string(surrounder)) * 2
		padding    = surrounded + separated
		gotPrefix  = "unexpected key: "
		wantPrefix = "want one of: "
		prefixes   = len(gotPrefix) + surrounded +
			len(wantPrefix) + separated
	)
	var (
		b    strings.Builder
		key  = fe.Key
		size = prefixes + len(key)
	)
	for i, tried := range fe.Tried {
		size += len(tried) + surrounded
		if i != 0 {
			size += separated
		}
	}
	b.Grow(size)
	b.WriteString(gotPrefix)
	b.WriteRune(surrounder)
	b.WriteString(key)
	b.WriteRune(surrounder)
	b.WriteString(separator)
	b.WriteString(wantPrefix)
	end := len(fe.Tried) - 1
	for i, tried := range fe.Tried {
		b.WriteRune(surrounder)
		b.WriteString(tried)
		b.WriteRune(surrounder)
		if i != end {
			b.WriteString(separator)
		}
	}
	return b.String()
}

func NewMountPoint[
	MP interface {
		*T
		MountPoint
	},
	T any,
](options ...MountPointOption,
) (p9.QID, *MountPointFile[MP], error) {
	var settings mountPointSettings
	if err := parseOptions(&settings, options...); err != nil {
		return p9.QID{}, nil, err
	}
	metadata, err := makeMetadata(p9.ModeRegular, settings.metaOptions...)
	if err != nil {
		return p9.QID{}, nil, err
	}
	linkSync, err := newLinkSync(settings.linkOptions...)
	if err != nil {
		return p9.QID{}, nil, err
	}
	file := &MountPointFile[MP]{
		mountPoint: new(T),
		mountPointFile: mountPointFile{
			metadata: metadata,
			linkSync: linkSync,
			mu:       new(sync.Mutex),
		},
		mountPointHost: mountPointHost{
			unmountFn: new(detachFunc),
		},
	}
	metadata.incrementPath()
	return *file.QID, file, nil
}

func (mf *MountPointFile[MP]) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	return mf.metadata.SetAttr(valid, attr)
}

func (mf *MountPointFile[MP]) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return mf.metadata.GetAttr(req)
}

func (mf *MountPointFile[MP]) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) > 0 {
		return nil, nil, perrors.ENOTDIR
	}
	if mf.opened() {
		return nil, nil, fidOpenedErr
	}
	return nil, &MountPointFile[MP]{
		mountPointFile: mf.mountPointFile,
		mountPointHost: mountPointHost{
			unmountFn: mf.unmountFn,
		},
		mountPoint: mf.mountPoint,
	}, nil
}

func (mf *MountPointFile[MP]) Open(mode p9.OpenFlags) (p9.QID, ioUnit, error) {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	if mf.opened() {
		return p9.QID{}, noIOUnit, perrors.EBADF
	}
	mf.openFlags = mf.withOpenedFlag(mode)
	return *mf.QID, noIOUnit, nil
}

func (mf *MountPointFile[MP]) WriteAt(p []byte, offset int64) (int, error) {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	if !mf.canWrite() {
		return -1, perrors.EBADF
	}
	if len(p) == 0 {
		return 0, nil
	}
	if offset == 0 { // Retain same mode on subsequent writes.
		mf.fieldMode = p[0] != '{'
	}
	var (
		written int
		err     error
	)
	if mf.fieldMode {
		written = len(p)
		err = mf.parseFieldsLocked(p)
	} else {
		written, err = mf.bufferStructuredLocked(p, offset)
	}
	if err != nil {
		return -1, err
	}
	return written, err
}

func (mf *MountPointFile[MP]) parseFieldsLocked(b []byte) error {
	const (
		key   = 0
		value = 1
	)
	for _, fields := range tokenize(b) {
		switch fields.typ() {
		case keyAndValue:
			var (
				key, value = fields[key], fields[value]
				mountPoint = mf.mountPoint
			)
			if err := mountPoint.ParseField(key, value); err != nil {
				return errors.Join(perrors.EINVAL, err)
			}
			mf.modified = true
		case keyWord:
			key := fields[key]
			if err := mf.parseKeyWordLocked(key); err != nil {
				return errors.Join(perrors.EINVAL, err)
			}
		default:
			// TODO: insert input into message? probably.
			return fmt.Errorf("%w - unexpected input", perrors.EINVAL)
		}
	}
	return nil
}

func (mf *MountPointFile[MP]) serializeLocked() ([]byte, error) {
	mb, err := json.Marshal(mf.mountPoint)
	if err != nil {
		return nil, err
	}
	return json.Marshal(mountPointMarshal{
		Data: json.RawMessage(mb),
		mountPointTag: mountPointTag{
			Host: mf.mountPoint.HostID(),
			ID:   mf.mountPoint.GuestID(),
		},
	})
}

func (mf *MountPointFile[MP]) parseKeyWordLocked(keyWord string) error {
	const syncKey = "sync"
	if keyWord == syncKey {
		return mf.syncLocked()
	}
	return FieldError{
		Key:   keyWord,
		Tried: []string{syncKey},
	}
	// TODO: Expected one of: $...
	// return fmt.Errorf("%w - invalid keyword: %s", perrors.EINVAL, keyWord)
}

func (mf *MountPointFile[MP]) bufferStructuredLocked(p []byte, offset int64) (int, error) {
	buffer := mf.buffer
	if buffer == nil {
		buffer = new(bytes.Buffer)
		mf.buffer = buffer
	}
	if dLen := buffer.Len(); offset != int64(dLen) {
		err := fmt.Errorf(
			"%w - structured input must append only",
			perrors.EINVAL,
		)
		return -1, err
	}
	mf.modified = true
	return buffer.Write(p)
}

func (mf *MountPointFile[MP]) FSync() error {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	return mf.syncLocked()
}

func (mf *MountPointFile[MP]) syncLocked() error {
	if !mf.modified {
		return nil
	}
	if err := mf.flushBufferLocked(); err != nil {
		return err
	}
	mf.modified = false
	data, err := mf.serializeLocked()
	if err != nil {
		return err
	}
	mf.Size = uint64(len(data))
	if err := mf.resetReaderLocked(data); err != nil {
		return err
	}
	return mf.remountLocked()
}

func (mf *MountPointFile[MP]) resetReaderLocked(data []byte) error {
	reader := mf.reader
	if reader == nil {
		return nil
	}
	offset, err := reader.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	reader.Reset(data)
	_, err = reader.Seek(offset, io.SeekStart)
	return err
}

func (mf *MountPointFile[MP]) flushBufferLocked() error {
	buffer := mf.buffer
	if buffer == nil ||
		buffer.Len() == 0 {
		return nil
	}
	defer buffer.Reset()
	data := buffer.Bytes()
	return json.Unmarshal(data, &mf.mountPoint)
}

func (mf *MountPointFile[MP]) remountLocked() error {
	if unmount := *mf.unmountFn; unmount != nil {
		if err := unmount(); err != nil {
			return err
		}
	}
	return mf.mountFileLocked()
}

func (mf *MountPointFile[MP]) mountFileLocked() error {
	goFS, err := mf.mountPoint.MakeFS()
	if err != nil {
		return err
	}
	closer, err := mf.mountPoint.Mount(goFS)
	if err == nil {
		*mf.unmountFn = closer.Close
		return nil
	}
	if parent := mf.linkSync.parent; parent != nil {
		const flags = 0
		child := mf.linkSync.child
		return errors.Join(
			perrors.EIO,
			err,
			parent.UnlinkAt(child, flags),
		)
	}
	return errors.Join(perrors.EIO, err)
}

func (mf *MountPointFile[MP]) ReadAt(p []byte, offset int64) (int, error) {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	reader := mf.reader
	if reader == nil {
		if !mf.canRead() {
			return -1, perrors.EBADF
		}
		data, err := mf.serializeLocked()
		if err != nil {
			// TODO: check spec for best errno
			return -1, errors.Join(perrors.EIO, err)
		}
		reader = bytes.NewReader(data)
		mf.reader = reader
	}
	return reader.ReadAt(p, offset)
}

func (mf *MountPointFile[MP]) Close() error {
	err := mf.FSync()
	mf.openFlags = 0
	mf.reader = nil
	mf.buffer = nil
	return err
}

func (mf *MountPointFile[MP]) detach() error {
	if detach := *mf.unmountFn; detach != nil {
		return detach()
	}
	return nil
}
