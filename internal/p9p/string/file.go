package string

import (
	"strings"
	"sync/atomic"

	"github.com/djdv/go-filesystem-utils/internal/p9p/errors"
	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
)

type (
	File struct {
		Names []string
		p9.QID
		p9.Attr
		Data *strings.Builder
		templatefs.NoopFile
	}
)

// TODO: path should be optional, if no parent then start at 0
// otherwise use parent's counter
// TODO: need optional parent node for walk ".."
func New(names []string, path *atomic.Uint64) (*File, p9.QID) {
	const placeholderDev = p9.Dev(2) // TODO from opts
	sf := &File{
		Names: names,
		QID: p9.QID{
			Type: p9.TypeRegular,
			Path: path.Add(1),
		},
		Attr: p9.Attr{
			Mode: p9.ModeRegular,
			// UID:  p9.NoUID,
			// GID:  p9.NoGID,
			UID:  0, // Hardcoded for root.
			GID:  0, // Hardcoded for root.
			RDev: placeholderDev,
		},
		Data: new(strings.Builder),
	}
	return sf, sf.QID
}

func (f *File) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	var (
		qid    = f.QID
		attr   p9.Attr
		filled p9.AttrMask
	)
	if req.Empty() {
		return qid, filled, attr, nil
	}

	if req.Mode {
		attr.Mode, filled.Mode = f.Attr.Mode, true
	}

	if req.UID {
		attr.UID, filled.UID = f.Attr.UID, true
	}
	if req.GID {
		attr.GID, filled.GID = f.Attr.GID, true
	}
	if req.GID {
		attr.GID, filled.GID = f.Attr.GID, true
	}
	if req.RDev {
		attr.RDev, filled.RDev = f.Attr.RDev, true
	}
	if req.Size {
		attr.Size, filled.Size = uint64(f.Data.Len()), true
	}

	return qid, filled, attr, nil
}

func (f *File) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	if valid.Size {
		var (
			curLen     = f.Data.Len()
			targetSize = int(attr.Size)
		)
		if curLen > targetSize {
			buf := f.Data.String()[:targetSize]
			f.Data.Reset()
			f.Data.WriteString(buf)
		}
		if curLen < targetSize {
			f.Data.Grow(targetSize - curLen)
		}
		f.Attr.Size = uint64(targetSize)
	}
	return nil
}

func (f *File) Walk(names []string) ([]p9.QID, p9.File, error) {
	// FIXME: we need to support ".." but currently don't.
	// (constructor needs an optional parent-node we can reference here)
	if len(names) > 0 {
		return nil, nil, errors.ENOTDIR // TODO: double check what Evalue the spec wants.
	}
	nf := new(File)
	*nf = *f
	return []p9.QID{nf.QID}, nf, nil
}

func (f *File) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	// TODO: prevent double open
	// [p9.Client] already handles this internally
	// but direct calls to [f] can currently violate the standard.
	return f.QID, 0, nil
}

func (f *File) WriteAt(p []byte, offset int64) (int, error) {
	// FIXME: fail if the file was not opened
	// [p9.Client] already handles this internally
	// but direct calls to [f] can currently violate the standard.

	// FIXME: offset not currently respected
	// Instead we just clobber everytime.
	f.Data.Reset()
	f.Version++
	n, err := f.Data.Write(p)
	f.Attr.Size = uint64(f.Data.Len())
	return n, err
}

func (f *File) ReadAt(p []byte, offset int64) (int, error) {
	// FIXME: fail if the file was not opened
	// [p9.Client] may (or may not) already handle this internally
	// but direct calls to [f] can currently violate the standard.
	return strings.NewReader(f.Data.String()).ReadAt(p, offset)
}
