package files

import (
	"bytes"
	"io"
	"sync/atomic"

	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

type (
	Closer struct {
		parent p9.File
		path   *atomic.Uint64
		closer io.Closer
		key    []byte

		p9.QID
		p9.Attr
		templatefs.NoopFile
		opened, armed bool
		// TODO: We're going to want to keep track of [existent].
		// For our case, fail all operations post removal.
		// Remove(5):
		// If a file has been opened as multiple fids, possibly on different connections,
		// and one fid is used to remove the file, whether the other fids continue
		// to provide access to the file is implementation-defined.
		// ^ .L replaces this with UnlinkAt.
		// Unless POSIX demands otherwise, use the same semantics for both.
	}
)

func NewCloser(closer io.Closer, options ...CloserOption) (*Closer, p9.QID) {
	sf := &Closer{
		closer: closer,
		QID:    p9.QID{Type: p9.TypeRegular}, // TODO: this should be configurable; specifically for [p9.TypeTemporary].
		Attr: p9.Attr{ // TODO: permissions from options (make sure to mask)
			// Mode: p9.ModeRegular | p9.Write, // TODO: default should be for owner?
			Mode: p9.ModeRegular | p9.AllPermissions,
			UID:  p9.NoUID,
			GID:  p9.NoGID,
		},
	}
	for _, setFunc := range options {
		if err := setFunc(sf); err != nil {
			panic(err)
		}
	}
	setupOrUsePather(&sf.QID.Path, &sf.path)
	return sf, sf.QID
}

func (cl *Closer) clone(with cloneQid) ([]p9.QID, *Closer) {
	var (
		qids  []p9.QID
		newCl = &Closer{
			parent: cl.parent,
			path:   cl.path,
			closer: cl.closer,
			QID:    cl.QID,
			Attr:   cl.Attr,
			key:    make([]byte, len(cl.key)),
		}
	)
	copy(newCl.key, cl.key)
	if with {
		qids = []p9.QID{newCl.QID}
	}
	return qids, newCl
}

func (cl *Closer) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error { return nil }

func (cl *Closer) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	var (
		qid          = cl.QID
		filled, attr = fillAttrs(req, &cl.Attr)
	)
	return qid, filled, *attr, nil
}

func (cl *Closer) Walk(names []string) ([]p9.QID, p9.File, error) {
	if cl.opened {
		return nil, nil, perrors.EINVAL // TODO: [spec] correct evalue?
	}
	switch wnames := len(names); wnames {
	case 0:
		_, nf := cl.clone(withoutQid)
		return nil, nf, nil
	case 1:
		switch names[0] {
		case parentWName:
			if parent := cl.parent; parent != nil {
				qid, _, _, err := cl.parent.GetAttr(p9.AttrMask{})
				return []p9.QID{qid}, parent, err
			}
			fallthrough
		case selfWName:
			qids, nf := cl.clone(withQid)
			return qids, nf, nil
		}
	}
	return nil, nil, perrors.ENOTDIR // TODO: [spec] correct evalue?
}

func (cl *Closer) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	if mode.Mode() != p9.WriteOnly {
		// TODO: [spec] correct evalue?
		return p9.QID{}, 0, perrors.EINVAL
	}
	if cl.opened {
		return p9.QID{}, 0, perrors.EBADF
	}
	cl.opened = true
	return cl.QID, 0, nil
}

func (cl *Closer) WriteAt(p []byte, offset int64) (int, error) {
	// FIXME: We don't currently handle partial writes.
	// Do some kind of buffer on open, delete on close, write-to here.
	// If the buffer matchers after a write, arm the file.
	if bytes.Equal(p, cl.key) {
		cl.armed = true
		return len(p), nil
	}
	return 0, perrors.EACCES // TODO: consider if this is an appropriate error
}

func (cl *Closer) UnlinkAt(name string, flags uint32) error {
	// TODO: incomplete impl; for testing
	if name != selfWName {
		return perrors.ENOTDIR
	}
	return cl.Remove()
}

func (cl *Closer) Remove() error {
	// TODO: incomplete impl; for testing
	if parent := cl.parent; parent != nil {
		return removeSelf(parent, cl)
	}
	return cl.Close()
}

func (cl *Closer) Close() error {
	cl.opened = false
	if cl.armed {
		// TODO: I think preventing double close is our responsability
		// not the closers.
		// Not likely to be a (silent) problem right now,
		// but needs to be fixed here. And documented in the constructor.
		// (Tell caller not to worry about double close, we'll prevent it.)
		return cl.closer.Close()
	}
	return nil
}
