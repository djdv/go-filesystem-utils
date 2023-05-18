package commands

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
	perrors "github.com/hugelgupf/p9/perrors"
)

// This name is arbitrary, but unlikely to collide.
// (a base58 NanoID of length 9)
// We use it as a special identifier to access
// an attach sessions error list.
//
// Rationale: the Plan 9 file protocol uses
// string values for errors. Unfortunately, the
// 9P2000 .u and .L variants use Unix `errorno`s
// for their error messages.
// These are sufficient for the operating system, but
// not for the system's operator(s). As a result,
// we store Go's native error values and allow clients
// to retrieve their string form via this file.
const errorsFileName = "⚠️ KsK5VBcSs"

type (
	attacher struct {
		path ninePath
		root p9.File
	}
	errorSys struct {
		file   p9.File
		path   ninePath
		errors *ringSync[error]
	}
	errorFile struct {
		templatefs.NoopFile
		errors *ringSync[error]
		reader *strings.Reader
		qid    p9.QID
	}
	ringSync[T any] struct {
		elements []T
		start    int
		length   int
		capacity int
		sync.Mutex
	}
)

var (
	_ p9.Attacher = (*attacher)(nil)
	_ p9.File     = (*errorSys)(nil)
	_ p9.File     = (*errorFile)(nil)
)

func receiveError(attachRoot p9.File, srvErr error) error {
	_, errs, err := attachRoot.Walk([]string{errorsFileName})
	fail := func(err error) error {
		return fmt.Errorf("errno: %w"+
			"\ncould not retrieve error string: %w",
			srvErr, err,
		)
	}
	if err != nil {
		return fail(err)
	}
	errorBytes, err := p9fs.ReadAll(errs)
	if err != nil {
		return fail(err)
	}
	if len(errorBytes) > 0 {
		return errors.New(string(errorBytes))
	}
	return srvErr
}

func newAttacher(path ninePath, root p9.File) *attacher {
	return &attacher{path: path, root: root}
}

func (at *attacher) Attach() (p9.File, error) {
	_, file, err := at.root.Walk(nil)
	if err != nil {
		return nil, err
	}
	// Our API client should never need this
	// much error history. We do few operations
	// after attach, then detach. But external
	// clients are potentially long lived, and
	// could instigate infinite errors in a
	// single session.
	// TODO: we should modify the 9P library
	// so that `Attach` gets passed the `AttachName`.
	// Then we can just have a special handshake
	// for our client that enables the error wrapping.
	// Everyone else would get direct access.
	// E.g. `Attach($errorsPrefix/$realName)`.
	const maxErrors = 10
	fsys := &errorSys{
		path:   at.path,
		file:   file,
		errors: newRing[error](maxErrors),
	}
	return fsys, nil
}

func (ef *errorSys) append(err error) {
	// Ignore errors that have no additional context.
	if _, ignore := err.(perrors.Errno); ignore {
		return
	}
	joinError, ok := err.(interface {
		Unwrap() []error
	})
	if !ok {
		ef.errors.append(err)
		return
	}
	for _, e := range joinError.Unwrap() {
		if _, ignore := e.(perrors.Errno); ignore {
			continue
		}
		ef.errors.append(e)
	}
}

func (ef *errorSys) wrap(file p9.File) p9.File {
	return &errorSys{
		path:   ef.path,
		file:   file,
		errors: ef.errors,
	}
}

func (ef *errorSys) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) == 1 && names[0] == errorsFileName {
		var (
			qid = p9.QID{
				Type: p9.TypeRegular,
				Path: ef.path.Add(1),
			}
			qids = []p9.QID{qid}
			file = &errorFile{
				qid:    qid,
				errors: ef.errors,
			}
		)
		return qids, file, nil
	}
	qids, file, err := ef.file.Walk(names)
	if err != nil {
		ef.append(err)
	}
	return qids, ef.wrap(file), err
}

func (ef *errorSys) WalkGetAttr(names []string) ([]p9.QID, p9.File, p9.AttrMask, p9.Attr, error) {
	qids, file, mask, attr, err := ef.file.WalkGetAttr(names)
	if err != nil {
		ef.append(err)
	}
	return qids, ef.wrap(file), mask, attr, err
}

func (ef *errorSys) StatFS() (p9.FSStat, error) {
	fsStat, err := ef.file.StatFS()
	if err != nil {
		ef.append(err)
	}
	return fsStat, err
}

func (ef *errorSys) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	qid, mask, attr, err := ef.file.GetAttr(req)
	if err != nil {
		ef.append(err)
	}
	return qid, mask, attr, err
}

func (ef *errorSys) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	err := ef.file.SetAttr(valid, attr)
	if err != nil {
		ef.append(err)
	}
	return err
}

func (ef *errorSys) Close() error {
	err := ef.file.Close()
	if err != nil {
		ef.append(err)
	}
	return err
}

func (ef *errorSys) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	qid, fd, err := ef.file.Open(mode)
	if err != nil {
		ef.append(err)
	}
	return qid, fd, err
}

func (ef *errorSys) ReadAt(p []byte, offset int64) (int, error) {
	n, err := ef.file.ReadAt(p, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		ef.append(err)
	}
	return n, err
}

func (ef *errorSys) WriteAt(p []byte, offset int64) (int, error) {
	n, err := ef.file.WriteAt(p, offset)
	if err != nil {
		ef.append(err)
	}
	return n, err
}

func (ef *errorSys) FSync() error {
	err := ef.file.FSync()
	if err != nil {
		ef.append(err)
	}
	return err
}

func (ef *errorSys) Create(name string, flags p9.OpenFlags, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.File, p9.QID, uint32, error) {
	file, qid, fd, err := ef.file.Create(name, flags, permissions, uid, gid)
	if err != nil {
		ef.append(err)
	}
	return ef.wrap(file), qid, fd, err
}

func (ef *errorSys) Mkdir(name string, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, error) {
	qid, err := ef.file.Mkdir(name, permissions, uid, gid)
	if err != nil {
		ef.append(err)
	}
	return qid, err
}

func (ef *errorSys) Symlink(oldName, newName string, uid p9.UID, gid p9.GID) (p9.QID, error) {
	qid, err := ef.file.Symlink(oldName, newName, uid, gid)
	if err != nil {
		ef.append(err)
	}
	return qid, err
}

func (ef *errorSys) Link(target p9.File, newName string) error {
	err := ef.file.Link(target, newName)
	if err != nil {
		ef.append(err)
	}
	return err
}

func (ef *errorSys) Mknod(name string, mode p9.FileMode, major, minor uint32, uid p9.UID, gid p9.GID) (p9.QID, error) {
	qid, err := ef.file.Mknod(name, mode, major, minor, uid, gid)
	if err != nil {
		ef.append(err)
	}
	return qid, err
}

func (ef *errorSys) Rename(newDir p9.File, newName string) error {
	err := ef.file.Rename(newDir, newName)
	if err != nil {
		ef.append(err)
	}
	return err
}

func (ef *errorSys) RenameAt(oldName string, newDir p9.File, newName string) error {
	err := ef.file.RenameAt(oldName, newDir, newName)
	if err != nil {
		ef.append(err)
	}
	return err
}

func (ef *errorSys) UnlinkAt(name string, flags uint32) error {
	err := ef.file.UnlinkAt(name, flags)
	if err != nil {
		ef.append(err)
	}
	return err
}

func (ef *errorSys) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	dirents, err := ef.file.Readdir(offset, count)
	if err != nil && !errors.Is(err, io.EOF) {
		ef.append(err)
	}
	return dirents, err
}

func (ef *errorSys) Readlink() (string, error) {
	link, err := ef.file.Readlink()
	if err != nil {
		ef.append(err)
	}
	return link, err
}

func (ef *errorSys) Renamed(newDir p9.File, newName string) {
	ef.file.Renamed(newDir, newName)
}

func (er *errorFile) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	var (
		qid   = er.qid
		valid p9.AttrMask
		attr  p9.Attr
	)
	if req.Mode {
		valid.Mode, attr.Mode = true, p9.ModeRegular
	}
	if req.Size {
		data := er.errorString()
		valid.Size, attr.Size = true, uint64(len(data))
	}
	return qid, valid, attr, nil
}

func (er *errorFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) > 0 {
		return nil, nil, perrors.ENOTDIR
	}
	if er.opened() {
		return nil, nil, perrors.EBUSY
	}
	clone := &errorFile{
		qid:    er.qid,
		errors: er.errors,
	}
	return nil, clone, nil
}

func (er *errorFile) opened() bool { return er.reader != nil }

func (er *errorFile) Open(p9.OpenFlags) (p9.QID, uint32, error) {
	if er.opened() {
		return p9.QID{}, 0, perrors.EBADF
	}
	data := er.errorString()
	er.reader = strings.NewReader(data)
	return er.qid, 0, nil
}

func (er *errorFile) errorString() string {
	var (
		errs   = er.errors.slice()
		unique = make([]string, 0, len(errs))
		set    = make(map[string]struct{})
	)
	for _, err := range errs {
		str := err.Error()
		if _, ok := set[str]; !ok {
			unique = append(unique, str)
			set[str] = struct{}{}
		}
	}
	return strings.Join(unique, "\n")
}

func (er *errorFile) ReadAt(p []byte, offset int64) (int, error) {
	if !er.opened() {
		return -1, perrors.EBADF
	}
	return er.reader.ReadAt(p, offset)
}

func newRing[T any](capacity int) *ringSync[T] {
	return &ringSync[T]{capacity: capacity}
}

func (rb *ringSync[T]) append(item T) {
	rb.Lock()
	defer rb.Unlock()
	if rb.elements == nil {
		rb.elements = make([]T, rb.capacity)
	}
	if rb.length < len(rb.elements) {
		rb.elements[(rb.start+rb.length)%len(rb.elements)] = item
		rb.length++
	} else {
		rb.elements[rb.start] = item
		rb.start = (rb.start + 1) % len(rb.elements)
	}
}

func (rb *ringSync[T]) slice() []T {
	rb.Lock()
	defer rb.Unlock()
	slice := make([]T, rb.length)
	for i := 0; i < rb.length; i++ {
		slice[i] = rb.elements[(rb.start+i)%len(rb.elements)]
	}
	return slice
}
