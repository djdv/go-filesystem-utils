package p9

import (
	"context"
	"encoding/json"
	"errors"
	"hash/maphash"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"time"
	"unsafe"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	p9net "github.com/djdv/go-filesystem-utils/internal/net/9p"
	perrors "github.com/djdv/p9/errors"
	"github.com/djdv/p9/fsimpl/templatefs"
	"github.com/djdv/p9/p9"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	Host struct {
		Maddr           multiaddr.Multiaddr `json:"maddr,omitempty"`
		ShutdownTimeout time.Duration       `json:"shutdownTimeout,omitempty"`
	}
	goAttacher struct {
		fsys fs.FS
		maphash.Hash
	}
	goFile struct {
		openFlags
		templatefs.NoopFile
		fsys   fs.FS
		file   fs.File
		names  []string
		p9.QID // TODO: the path value for this isn't spec compliant
		// "The path is an integer unique among all files in the hierarchy. If a file is deleted and recreated with the same name in the same directory, the old and new path components of the qids should be different." intro (5)
		// We can keep track of changes /we/ make
		// and modify some path salt
		// (global map[paths-hash]atomicInt |> hasher.append)
		// but since `fs.FS` has no unique number like path, ino, etc.
		// or even creation date, we won't know if someone else
		// created a new file with the same path-names.
		// tracking ops+birthtime will be best effort.
		cursor   uint64
		hashSeed maphash.Seed
	}
)

const HostID filesystem.Host = "9P"

func (*Host) HostID() filesystem.Host { return HostID }

func (h9 *Host) UnmarshalJSON(b []byte) error {
	// multiformats/go-multiaddr issue #100
	var maddrWorkaround struct {
		Maddr multiaddrContainer `json:"maddr,omitempty"`
	}
	if err := json.Unmarshal(b, &maddrWorkaround); err != nil {
		return err
	}
	h9.Maddr = maddrWorkaround.Maddr.Multiaddr
	return nil
}

func (h9 *Host) Mount(fsys fs.FS) (io.Closer, error) {
	listener, err := manet.Listen(h9.Maddr)
	if err != nil {
		return nil, err
	}
	attacher := &goAttacher{
		fsys: fsys,
	}
	var (
		l = log.New(os.Stdout, "srv9 ", log.Lshortfile)
		// TODO: opts passthrough.
		options = []p9net.ServerOpt{
			p9net.WithServerLogger(l),
		}
		server = p9net.NewServer(attacher, options...)
		srvErr = make(chan error, 1)
	)
	go func() {
		defer close(srvErr)
		err := server.Serve(listener)
		if err == nil ||
			errors.Is(err, p9net.ErrServerClosed) {
			return
		}
		srvErr <- err
	}()
	if h9.ShutdownTimeout == 0 {
		return generic.Closer(server.Close), nil
	}
	var closer generic.Closer = func() error {
		ctx, cancel := context.WithTimeout(
			context.Background(),
			h9.ShutdownTimeout,
		)
		defer cancel()
		return errors.Join(
			server.Shutdown(ctx),
			<-srvErr,
		)
	}
	return closer, nil
}

func (a9 *goAttacher) Attach() (p9.File, error) {
	return &goFile{
		fsys: a9.fsys,
		QID: p9.QID{
			Type: p9.TypeDir,
			Path: a9.Hash.Sum64(),
		},
		hashSeed: a9.Hash.Seed(),
	}, nil
}

func (f9 *goFile) goName(names ...string) string {
	if len(f9.names) == 0 {
		return filesystem.Root
	}
	return path.Join(append(f9.names, names...)...)
}

func (f9 *goFile) makeHasher() (hasher maphash.Hash, err error) {
	hasher.SetSeed(f9.hashSeed)
	err = f9.hashNames(&hasher)
	return
}

func (f9 *goFile) hashNames(hasher *maphash.Hash) error {
	for _, name := range f9.names {
		if _, err := hasher.WriteString(name); err != nil {
			return err
		}
	}
	return nil
}

func (f9 *goFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) == 0 {
		if f9.opened() {
			return nil, nil, fidOpenedErr
		}
		file := &goFile{
			fsys:     f9.fsys,
			hashSeed: f9.hashSeed,
			QID:      f9.QID,
			names:    f9.names,
		}
		return nil, file, nil
	}
	hasher, err := f9.makeHasher()
	if err != nil {
		return nil, nil, err
	}
	qids := make([]p9.QID, len(names))
	for i, name := range names {
		info, err := fs.Stat(f9.fsys, f9.goName(names[:i+1]...))
		if err != nil {
			return qids[:i], nil, err
		}
		if _, err := hasher.WriteString(name); err != nil {
			return qids[:i], nil, err
		}
		qids[i] = p9.QID{
			Type: goToQIDType(info.Mode().Type()),
			Path: hasher.Sum64(),
		}
	}
	file := &goFile{
		fsys:     f9.fsys,
		hashSeed: f9.hashSeed,
		QID:      qids[len(qids)-1],
		names:    append(f9.names, names...),
	}
	return qids, file, nil
}

func goToQIDType(typ fs.FileMode) p9.QIDType {
	switch typ {
	default:
		return p9.TypeRegular
	case fs.ModeDir:
		return p9.TypeDir
	case fs.ModeAppend:
		return p9.TypeAppendOnly
	case fs.ModeExclusive:
		return p9.TypeExclusive
	case fs.ModeTemporary:
		return p9.TypeTemporary
	case fs.ModeSymlink:
		return p9.TypeSymlink
	}
}

func (f9 *goFile) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	var (
		attr      p9.Attr
		valid     p9.AttrMask
		info, err = fs.Stat(f9.fsys, f9.goName())
	)
	if err != nil {
		return f9.QID, valid, attr, err
	}
	attr.Mode, valid.Mode = p9.ModeFromOS(info.Mode()), true
	attr.Size, valid.Size = uint64(info.Size()), true
	var (
		modTime = info.ModTime()
		mSec    = uint64(modTime.Unix())
		mNSec   = uint64(modTime.UnixNano())
	)
	attr.MTimeSeconds, attr.MTimeNanoSeconds,
		valid.MTime = mSec, mNSec, true
	if atimer, ok := info.(filesystem.AccessTimeInfo); ok {
		var (
			accessTime = atimer.AccessTime()
			aSec       = uint64(accessTime.Unix())
			aNSec      = uint64(accessTime.UnixNano())
		)
		attr.ATimeSeconds, attr.ATimeNanoSeconds,
			valid.ATime = aSec, aNSec, true
	}
	if ctimer, ok := info.(filesystem.ChangeTimeInfo); ok {
		var (
			changeTime = ctimer.ChangeTime()
			cSec       = uint64(changeTime.Unix())
			cNSec      = uint64(changeTime.UnixNano())
		)
		attr.CTimeSeconds, attr.CTimeNanoSeconds,
			valid.CTime = cSec, cNSec, true
	}
	if crtimer, ok := info.(filesystem.CreationTimeInfo); ok {
		var (
			birthTime = crtimer.CreationTime()
			bSec      = uint64(birthTime.Unix())
			bNSec     = uint64(birthTime.UnixNano())
		)
		attr.BTimeSeconds, attr.BTimeNanoSeconds,
			valid.BTime = bSec, bNSec, true
	}
	return f9.QID, valid, attr, nil
}

func (f9 *goFile) Open(mode p9.OpenFlags) (p9.QID, ioUnit, error) {
	if f9.opened() {
		return f9.QID, 0, perrors.EINVAL
	}
	var (
		file fs.File
		err  error
		name = f9.goName()
	)
	if mode.Mode() == p9.ReadOnly {
		file, err = f9.fsys.Open(name)
	} else {
		opener, ok := f9.fsys.(filesystem.OpenFileFS)
		if !ok {
			return f9.QID, 0, perrors.EROFS
		}
		// TODO: mode conversion - 9P.L to OS independent representation
		file, err = opener.OpenFile(name, int(mode), 0)
	}
	if err != nil {
		return p9.QID{}, 0, err
	}
	f9.file = file
	f9.openFlags = f9.withOpenedFlag(mode)
	return f9.QID, noIOUnit, nil
}

func (f9 *goFile) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	if !f9.canRead() {
		return nil, perrors.EBADF
	}
	directory, ok := f9.file.(fs.ReadDirFile)
	if !ok {
		return nil, perrors.ENOTDIR
	}
	if offset != f9.cursor {
		return nil, perrors.ENOENT
	}
	const entrySize = unsafe.Sizeof(p9.Dirent{})
	countGo := int(count / uint32(entrySize)) // Bytes -> index.
	ents, err := directory.ReadDir(countGo)
	if err != nil {
		if errors.Is(err, io.EOF) {
			err = nil
		}
		return nil, err
	}
	var (
		entryOffset = f9.cursor + 1
		entryCount  = len(ents)
	)
	f9.cursor += uint64(entryCount)
	entries := make(p9.Dirents, entryCount)
	hasher, err := f9.makeHasher()
	if err != nil {
		return nil, err
	}
	end := entryCount - 1
	for i, ent := range ents {
		var (
			name = ent.Name()
			typ  = goToQIDType(ent.Type())
		)
		if _, err := hasher.WriteString(name); err != nil {
			return nil, err
		}
		entries[i] = p9.Dirent{
			Name: name,
			QID: p9.QID{
				Type: typ,
				Path: hasher.Sum64(),
			},
			Offset: entryOffset,
			Type:   typ,
		}
		if i == end {
			break
		}
		entryOffset++
		hasher.Reset()
		if err := f9.hashNames(&hasher); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func (f9 *goFile) ReadAt(p []byte, offset int64) (int, error) {
	if !f9.canRead() {
		return -1, perrors.EBADF
	}
	var (
		file       = f9.file
		seeker, ok = file.(io.Seeker)
	)
	if !ok {
		return -1, perrors.ESPIPE
	}
	if _, err := seeker.Seek(offset, io.SeekStart); err != nil {
		return -1, err
	}
	return file.Read(p)
}

func (f9 *goFile) Close() error {
	f9.openFlags = 0
	if file := f9.file; file != nil {
		f9.file = nil
		return file.Close()
	}
	return nil
}
