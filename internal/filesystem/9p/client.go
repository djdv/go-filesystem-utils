package p9

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"
	"unsafe"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	perrors "github.com/djdv/p9/errors"
	"github.com/djdv/p9/fsimpl/templatefs"
	"github.com/djdv/p9/p9"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	Guest struct {
		Maddr multiaddr.Multiaddr `json:"maddr,omitempty"`
	}
	plan9FS struct {
		client *p9.Client
		root   p9.File
	}
	plan9File struct {
		walkFID, ioFID p9.File
		name           string
		cursor         int64
	}
	lazyIO struct {
		templatefs.NoopFile
		container *plan9File
		p9.OpenFlags
	}
	plan9Info struct {
		attr *p9.Attr
		name string
	}
	plan9Entry struct {
		*p9.Dirent
		parent     p9.File
		parentName string
	}
)

var (
	_ fs.FS           = (*plan9FS)(nil)
	_ fs.StatFS       = (*plan9FS)(nil)
	_ filesystem.IDFS = (*plan9FS)(nil)
	_ fs.File         = (*plan9File)(nil)
	_ fs.FileInfo     = (*plan9Info)(nil)
)

const (
	GuestID         filesystem.ID = "9P"
	pathSeparatorGo               = "/"
)

func (*Guest) GuestID() filesystem.ID { return GuestID }
func (g9 *Guest) MakeFS() (fs.FS, error) {
	conn, err := manet.Dial(g9.Maddr)
	if err != nil {
		return nil, err
	}
	return NewPlan9Guest(conn)
}

// TODO: Options:
// - Client log
func NewPlan9Guest(channel io.ReadWriteCloser) (*plan9FS, error) {
	client, err := p9.NewClient(channel)
	if err != nil {
		return nil, err
	}
	root, err := client.Attach("")
	if err != nil {
		return nil, err
	}
	fsys := plan9FS{
		client: client,
		root:   root,
	}
	return &fsys, nil
}

func (*plan9FS) ID() filesystem.ID { return GuestID }

func (fsys *plan9FS) Stat(name string) (fs.FileInfo, error) {
	const op = "stat"
	file, err := fsys.walkTo(op, name)
	if err != nil {
		return nil, err
	}
	info, err := getInfoGo(name, file)
	if err := errors.Join(err, file.Close()); err != nil {
		return nil, fserrors.New(op, name, err, fserrors.IO)
	}
	return info, err
}

func (fsys *plan9FS) walkTo(op, name string) (p9.File, error) {
	if !fs.ValidPath(name) {
		return nil, fserrors.New(op, name, filesystem.ErrPath, fserrors.InvalidItem)
	}
	var names []string
	if name != filesystem.Root {
		names = strings.Split(name, pathSeparatorGo)
	}
	_, file, err := fsys.root.Walk(names)
	if err != nil {
		var kind fserrors.Kind
		if errors.Is(err, perrors.ENOENT) {
			kind = fserrors.NotExist
		} else {
			kind = fserrors.IO
		}
		return nil, fserrors.New(op, name, err, kind)
	}
	return file, nil
}

func (fsys *plan9FS) Open(name string) (fs.File, error) {
	const op = "open"
	walkFID, err := fsys.walkTo(op, name)
	if err != nil {
		return nil, err
	}
	var wrapper plan9File //🥚 Self referential.
	wrapper = plan9File{
		name:    name,
		walkFID: walkFID,
		ioFID: &lazyIO{
			container: &wrapper, // 🐣 Self referential.
			OpenFlags: p9.ReadOnly,
		},
	}
	return &wrapper, nil
}

func (fsys *plan9FS) Close() error {
	var errs []error
	for _, closer := range []io.Closer{
		fsys.root,
		fsys.client,
	} {
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (f9 *plan9File) Stat() (fs.FileInfo, error) {
	return getInfoGo(f9.name, f9.walkFID)
}

func (f9 *plan9File) Read(p []byte) (int, error) {
	n, err := f9.ioFID.ReadAt(p, f9.cursor)
	if err == nil {
		f9.cursor += int64(n)
	}
	return n, err
}

func (f9 *plan9File) ReadDir(count int) ([]fs.DirEntry, error) {
	const entrySize = unsafe.Sizeof(p9.Dirent{})
	count9 := count * int(entrySize) // Index -> bytes.
	entries9, err := f9.ioFID.Readdir(uint64(f9.cursor), uint32(count9))
	if err != nil {
		return nil, err
	}
	limit := len(entries9)
	if limit == 0 && count > 0 {
		return nil, io.EOF
	}
	if count > 0 && limit > count {
		limit = count
	}
	var (
		entries    = make([]fs.DirEntry, limit)
		parent     = f9.walkFID
		parentName = f9.name
	)
	for i := range entries {
		entries[i] = plan9Entry{
			parent:     parent,
			parentName: parentName,
			Dirent:     &entries9[i],
		}
	}
	f9.cursor += int64(len(entries))
	return entries, nil
}

func (f9 *plan9File) Seek(offset int64, whence int) (int64, error) {
	const op = "seek"
	switch whence {
	case io.SeekStart:
		if offset < 0 {
			err := generic.ConstError(
				"tried to seek to a position before the beginning of the file",
			)
			return -1, fserrors.New(
				op, f9.name,
				err, fserrors.InvalidItem,
			)
		}
		f9.cursor = offset
	case io.SeekCurrent:
		f9.cursor += offset
	case io.SeekEnd:
		var (
			want      = p9.AttrMask{Size: true}
			info, err = getInfo(f9.name, f9.walkFID, want)
		)
		if err != nil {
			return -1, err
		}
		end := info.attr.Size
		f9.cursor = int64(end) + offset
	}
	return f9.cursor, nil
}

func (f9 *plan9File) Close() error {
	return errors.Join(
		f9.ioFID.Close(),
		f9.walkFID.Close(),
	)
}

func (i9 *plan9Info) Name() string      { return i9.name }
func (i9 *plan9Info) Size() int64       { return int64(i9.attr.Size) }
func (i9 *plan9Info) Mode() fs.FileMode { return i9.attr.Mode.OSMode() }
func (i9 *plan9Info) ModTime() time.Time {
	return time.Unix(0, int64(i9.attr.MTimeNanoSeconds))
}
func (i9 *plan9Info) IsDir() bool { return i9.Mode().IsDir() }

func (i9 *plan9Info) Sys() any { return i9 }

func (g9 *Guest) UnmarshalJSON(b []byte) error {
	// multiformats/go-multiaddr issue #100
	var maddrWorkaround struct {
		Maddr multiaddrContainer `json:"maddr,omitempty"`
	}
	if err := json.Unmarshal(b, &maddrWorkaround); err != nil {
		return err
	}
	g9.Maddr = maddrWorkaround.Maddr.Multiaddr
	return nil
}

func (e9 plan9Entry) Name() string { return e9.Dirent.Name }
func (e9 plan9Entry) IsDir() bool  { return e9.Dirent.Type == p9.TypeDir }
func (e9 plan9Entry) Type() fs.FileMode {
	switch e9.Dirent.Type {
	case p9.TypeRegular:
		return fs.FileMode(0)
	case p9.TypeDir:
		return fs.ModeDir
	case p9.TypeAppendOnly:
		return fs.ModeAppend
	case p9.TypeExclusive:
		return fs.ModeExclusive
	case p9.TypeTemporary:
		return fs.ModeTemporary
	case p9.TypeSymlink:
		return fs.ModeSymlink
	default:
		return fs.ModeIrregular
	}
}

func (e9 plan9Entry) Info() (fs.FileInfo, error) {
	wnames := []string{e9.Dirent.Name}
	_, file, err := e9.parent.Walk(wnames)
	if err != nil {
		return nil, err
	}
	var (
		name = path.Join(e9.parentName, e9.Dirent.Name)
		errs []error
	)
	info, err := getInfoGo(name, file)
	if err != nil {
		errs = append(errs, err)
	}
	if err := file.Close(); err != nil {
		errs = append(errs, err)
	}
	return info, errors.Join(errs...)
}

func getInfoGo(name string, file p9.File) (*plan9Info, error) {
	return getInfo(name, file, p9.AttrMask{
		Mode:  true,
		Size:  true,
		MTime: true,
	})
}

func getInfo(name string, file p9.File, want p9.AttrMask) (*plan9Info, error) {
	_, valid, attr, err := file.GetAttr(want)
	const op = "stat"
	if err == nil &&
		!valid.Contains(want) {
		err = attrErr(valid, want)
	}
	if err != nil {
		return nil, fserrors.New(op, name, err, fserrors.IO)
	}
	return &plan9Info{
		name: name,
		attr: &attr,
	}, nil
}

func (lio *lazyIO) initAndSwapIO() (p9.File, error) {
	container := lio.container
	_, clone, err := container.walkFID.Walk(nil)
	if err != nil {
		return nil, err
	}
	if _, _, err := clone.Open(lio.OpenFlags); err != nil {
		if cErr := clone.Close(); cErr != nil {
			return nil, errors.Join(err, cErr)
		}
		return nil, err
	}
	container.ioFID = clone
	return clone, nil
}

func (lio *lazyIO) ReadAt(p []byte, offset int64) (int, error) {
	file, err := lio.initAndSwapIO()
	if err != nil {
		return -1, err
	}
	return file.ReadAt(p, offset)
}

func (lio *lazyIO) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	file, err := lio.initAndSwapIO()
	if err != nil {
		return nil, err
	}
	return file.Readdir(offset, count)
}
