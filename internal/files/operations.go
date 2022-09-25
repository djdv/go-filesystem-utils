package files

import (
	"errors"
	"io"
	"math"

	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

/* TODO: maybe scrap this.
type WalkDirFunc func(path string, d p9.Dirent, err error) error

// TODO: dedupe mkdirall, removeEmpties, flattenX, et al. with this.
func WalkDir(fsys p9.File, fn WalkDirFunc) error {
	var (
		closers  = make([]io.Closer, 0, len(names))
		closeAll = func() error {
			for _, c := range closers {
				if err := c.Close(); err != nil {
					return err
				}
			}
			closers = nil
			return nil
		}
	)
	defer closeAll() // TODO: error needs to be caught and appended if we return early.
	// TODO: this could be real-time (callbacks or channels vs slices).
	files, err := flattenDir(fsys)
	if err != nil {
		return err
	}
	for _, f := range files {
		// TODO: signature isn't currently very useful for us
	}
	return closeAll()
}

func flattenDir(dir p9.File) ([]p9.File, error) {
	ents, err := ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var (
		files = make([]p9.File, 0, len(ents))
		// TODO: micro-opt; is this faster than allocating in the loop?
		wnames = make([]string, 1)
	)
	for _, ent := range ents {
		wnames[0] = ent.Name
		_, entFile, err := dir.Walk(wnames)
		if err != nil {
			return nil, err
		}
		if ent.Type == p9.TypeDir {
			submaddrs, err := flattenDir(entFile)
			if err != nil {
				return nil, err
			}
			files = append(files, submaddrs...)
			continue
		}
		files = append(files, entFile)
	}
	return files, nil
}
*/

func MkdirAll(root p9.File, names []string,
	permissions p9.FileMode, uid p9.UID, gid p9.GID,
) (p9.File, error) {
	var (
		closers  = make([]io.Closer, 0, len(names))
		closeAll = func() error {
			for _, c := range closers {
				if err := c.Close(); err != nil {
					return err
				}
			}
			closers = nil
			return nil
		}
	)
	defer closeAll() // TODO: error needs to be caught and appended if we return early.
	var (
		tail   = len(names) - 1
		wnames = make([]string, 1)
		next   = root
	)
	for i, name := range names {
		wnames[0] = name
		_, nextF, err := next.Walk(wnames)
		if err != nil {
			if !errors.Is(err, perrors.ENOENT) {
				return nil, err
			}
			if _, err := next.Mkdir(name, permissions, uid, gid); err != nil {
				return nil, err
			}
			if _, nextF, err = next.Walk(wnames); err != nil {
				return nil, err
			}
		}
		if i != tail {
			closers = append(closers, nextF)
		}
		next = nextF
	}
	if err := closeAll(); err != nil {
		return nil, err
	}
	return next, nil
}

// TODO: export this? But where? What name? ReaddirAll?
// *We're using the same name as [os] (new canon)
// and [fs] (newer canon) for now, make sure this causes no issues.
func ReadDir(dir p9.File) (_ p9.Dirents, err error) {
	_, dirClone, err := dir.Walk(nil)
	if err != nil {
		return nil, err
	}
	if _, _, err := dirClone.Open(p9.ReadOnly); err != nil {
		return nil, err
	}
	defer func() {
		cErr := dirClone.Close()
		if err == nil {
			err = cErr
		}
	}()
	var (
		offset uint64
		ents   p9.Dirents
	)
	for { // TODO: [Ame] double check correctness (offsets and that)
		entBuf, err := dirClone.Readdir(offset, math.MaxUint32)
		if err != nil {
			return nil, err
		}
		bufferedEnts := len(entBuf)
		if bufferedEnts == 0 {
			break
		}
		offset = entBuf[bufferedEnts-1].Offset
		ents = append(ents, entBuf...)
	}
	return ents, nil
}

func ReadAll(file p9.File) (_ []byte, err error) {
	// TODO: walkgetattr with fallback.
	_, fileClone, err := file.Walk(nil)
	if err != nil {
		return nil, err
	}

	want := p9.AttrMask{Size: true}
	_, valid, attr, err := fileClone.GetAttr(want)
	if err != nil {
		return nil, err
	}
	if !valid.Contains(want) {
		return nil, attrErr(valid, want)
	}

	if _, _, err := fileClone.Open(p9.ReadOnly); err != nil {
		return nil, err
	}
	defer func() {
		cErr := fileClone.Close()
		if err == nil {
			err = cErr
		}
	}()
	sr := io.NewSectionReader(fileClone, 0, int64(attr.Size))
	return io.ReadAll(sr)
}
