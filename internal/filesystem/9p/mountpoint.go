package p9

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	perrors "github.com/djdv/p9/errors"
	"github.com/djdv/p9/fsimpl/templatefs"
	"github.com/djdv/p9/p9"
)

type (
	detachFunc = func() error
	Mountpoint interface {
		mountpoint.FSMaker
		mountpoint.Mounter
		mountpoint.Marshaler
		mountpoint.FieldParser
	}
	MountpointFile struct {
		templatefs.NoopFile
		*metadata
		mu       *sync.Mutex
		linkSync *linkSync
		// All FID instances must be able to
		// read and write this function pointer.
		// Hence `*func` instead of just `func`.
		detachFn   *detachFunc
		mountpoint Mountpoint
		reader     *bytes.Reader
		buffer     *bytes.Buffer
		openFlags
		fieldMode, modified, closing bool
	}
	MountPointOption func(*fileSettings) error
)

func NewMountpointFile(mountpoint Mountpoint, options ...MountPointOption,
) (p9.QID, *MountpointFile, error) {
	var settings fileSettings
	settings.metadata.initialize(p9.ModeRegular)
	if err := generic.ApplyOptions(&settings, options...); err != nil {
		return p9.QID{}, nil, err
	}
	file := &MountpointFile{
		mountpoint: mountpoint,
		metadata:   &settings.metadata,
		linkSync:   &settings.linkSync,
		mu:         new(sync.Mutex),
		detachFn:   new(detachFunc),
	}
	settings.metadata.fillDefaults()
	settings.metadata.incrementPath()
	return settings.QID, file, nil
}

func (mf *MountpointFile) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	return mf.metadata.SetAttr(valid, attr)
}

func (mf *MountpointFile) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return mf.metadata.GetAttr(req)
}

func (mf *MountpointFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) > 0 {
		return nil, nil, perrors.ENOTDIR
	}
	if mf.opened() {
		return nil, nil, fidOpenedErr
	}
	return nil, &MountpointFile{
		metadata:   mf.metadata,
		mu:         mf.mu,
		linkSync:   mf.linkSync,
		detachFn:   mf.detachFn,
		mountpoint: mf.mountpoint,
	}, nil
}

func (mf *MountpointFile) Open(mode p9.OpenFlags) (p9.QID, ioUnit, error) {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	if mf.opened() {
		return p9.QID{}, noIOUnit, perrors.EBADF
	}
	mf.openFlags = mf.withOpenedFlag(mode)
	return mf.QID, noIOUnit, nil
}

func (mf *MountpointFile) WriteAt(p []byte, offset int64) (int, error) {
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

func (mf *MountpointFile) parseFieldsLocked(b []byte) error {
	for _, attrValue := range tokenize(b) {
		attribute := attrValue[attribute]
		if attrValue.attributeOnly() {
			if err := mf.parseAttributeLocked(attribute); err != nil {
				return errors.Join(perrors.EINVAL, err)
			}
			continue
		}
		if err := mf.mountpoint.ParseField(
			attribute, attrValue[value],
		); err != nil {
			return errors.Join(perrors.EINVAL, err)
		}
		mf.modified = true
	}
	return nil
}

func (mf *MountpointFile) parseAttributeLocked(keyWord string) error {
	const syncKey = "sync"
	if keyWord == syncKey {
		return mf.syncLocked()
	}
	return mountpoint.FieldError{
		Attribute: keyWord,
		Tried:     []string{syncKey},
	}
	// TODO: Expected one of: $...
	// return fmt.Errorf("%w - invalid keyword: %s", perrors.EINVAL, keyWord)
}

func (mf *MountpointFile) bufferStructuredLocked(p []byte, offset int64) (int, error) {
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

func (mf *MountpointFile) serializeLocked() ([]byte, error) {
	return mf.mountpoint.MarshalJSON()
}

func (mf *MountpointFile) FSync() error {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	return mf.syncLocked()
}

func (mf *MountpointFile) syncLocked() error {
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

func (mf *MountpointFile) resetReaderLocked(data []byte) error {
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

func (mf *MountpointFile) flushBufferLocked() error {
	buffer := mf.buffer
	if buffer == nil ||
		buffer.Len() == 0 {
		return nil
	}
	defer buffer.Reset()
	return mf.mountpoint.UnmarshalJSON(buffer.Bytes())
}

func (mf *MountpointFile) remountLocked() error {
	if detachFn := *mf.detachFn; detachFn != nil {
		if err := detachFn(); err != nil {
			return err
		}
	}
	return mf.mountFileLocked()
}

func (mf *MountpointFile) mountFileLocked() error {
	goFS, err := mf.mountpoint.MakeFS()
	if err != nil {
		return err
	}
	closer, err := mf.mountpoint.Mount(goFS)
	if err != nil {
		if cErr := filesystem.Close(goFS); cErr != nil {
			return errors.Join(err, cErr)
		}
		return err
	}
	*mf.detachFn = closer.Close
	return nil
}

func (mf *MountpointFile) failAndUnlinkSelf(err error) error {
	errs := []error{
		perrors.EIO,
		err,
	}
	if parent := mf.linkSync.parent; parent != nil {
		const flags = 0
		child := mf.linkSync.child
		if err = parent.UnlinkAt(child, flags); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (mf *MountpointFile) ReadAt(p []byte, offset int64) (int, error) {
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

func (mf *MountpointFile) Close() error {
	mf.mu.Lock()
	err := mf.syncLocked()
	mf.mu.Unlock()
	if err != nil {
		return mf.failAndUnlinkSelf(err)
	}
	return err
}

func (mf *MountpointFile) detach() error {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	if detach := *mf.detachFn; detach != nil {
		return detach()
	}
	return nil
}
