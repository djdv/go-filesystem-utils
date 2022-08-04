package files

import (
	"math"

	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
	"golang.org/x/exp/constraints"
)

type (
	cloneQid bool

	devClass    = uint32
	devInstance = uint32
)

const apiDev devClass = iota

const (
	shutdownInst devInstance = iota
	motdInst
)

const (
	withoutQid cloneQid = false
	withQid    cloneQid = true

	selfWName   = "."
	parentWName = ".."
)

// TODO: is this in the standard somewhere yet?
func max[T constraints.Ordered](x, y T) T {
	if x > y {
		return x
	}
	return y
}

// TODO: is this in the standard somewhere yet?
func min[T constraints.Ordered](x, y T) T {
	if x < y {
		return x
	}
	return y
}

func removeSelf(parent, self p9.File) error {
	names, err := ReadDir(parent)
	if err != nil {
		return err
	}
	// TODO: we can trade lookups for memory and short circuit here
	// if that's sensible.
	// if self.Namer; return parent.UnlinkAt(self.Name())
	// Otherwise we have to find ourself within our parent directory.
	// They're the ones storing link/name-data.

	var (
		sawError bool
		// TODO: micro-opt; is this faster than allocating in the loop?
		wname = make([]string, 1)
	)
	for _, dirent := range names {
		name := dirent.Name
		wname[0] = name
		_, file, err := parent.Walk(wname)
		if err != nil {
			sawError = true // Might not be us, take note
			continue        // but ignore it.
		}
		// TODO: closes should be handled properly,
		// and especially not deferred within the loop.
		// Only slightly better than relying on the finalizer.
		defer file.Close()
		if file == self {
			return parent.UnlinkAt(name, 0)
		}
	}
	if sawError {
		// Walk errors could have been for us,
		// no way to know so bail hard(er).
		return perrors.EIO
	}
	return perrors.ENOENT
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
