package files

import (
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

// TODO: sort and move code in this file to more appropriate places.

const (
	selfWName   = "."
	parentWName = ".."
)

type (
	childWalker[F p9.File] interface {
		parent() p9.File
	}
	fileWalker[F p9.File] interface {
		fidOpened() bool
		clone(withQid bool) ([]p9.QID, F, error)
	}
	dirWalker[F p9.File] interface {
		fileWalker[F]
		files() fileTable
	}
)

func walk[F p9.File](file fileWalker[F], names ...string) ([]p9.QID, p9.File, error) {
	if file.fidOpened() {
		return nil, nil, perrors.EINVAL // TODO: [spec] correct evalue?
	}
	const (
		withoutQID = false
		withQID    = true
	)
	switch nameCount := len(names); nameCount {
	case 0:
		return file.clone(withoutQID)
	case 1:
		switch names[0] {
		case parentWName:
			if child, ok := file.(childWalker[F]); ok {
				return child.parent().Walk([]string{selfWName})
			}
			fallthrough
		case selfWName:
			return file.clone(withQID)
		}
	}
	if dir, ok := file.(dirWalker[F]); ok {
		return walkRecur(dir.files(), names...)
	}
	return nil, nil, perrors.ENOTDIR
}

func walkRecur(files fileTable, names ...string) ([]p9.QID, p9.File, error) {
	file, ok := files.load(names[0])
	if !ok {
		return nil, nil, perrors.ENOENT
	}
	qids := make([]p9.QID, 1, len(names))
	qid, _, _, err := file.GetAttr(p9.AttrMask{})
	if err != nil {
		return nil, nil, err
	}
	if qids[0] = qid; len(qids) == cap(qids) {
		return qids, file, nil
	}

	subNames := names[1:]
	subQids, subFile, err := file.Walk(subNames)
	if err != nil {
		return nil, nil, err
	}
	return append(qids, subQids...), subFile, nil
}

// XXX this whole thang is likely more nasty than it has to be.
// If anything fails in here we're likely going to get zombie files that might ruin things.
// Likely fine for empty directories, but not endpoints. That shouldn't happen though.
// "shouldn't"
// TODO: name needs to imply reverse order, or take an order param
func removeEmpties(root p9.File, dirs []string) error {
	var (
		cur      = root
		nwname   = len(dirs)
		dirFiles = make([]p9.File, nwname)

		// TODO: micro-opt; is this faster than allocating in the loop?
		wname = make([]string, 1)
	)
	for i, name := range dirs {
		wname[0] = name
		_, dir, err := cur.Walk(wname)
		if err != nil {
			return err
		}
		cur = dir
		dirFiles[i] = cur
	}
	for i := nwname - 1; i >= 0; i-- {
		cur := dirFiles[i]
		ents, err := ReadDir(cur)
		if err != nil {
			return err
		}
		if len(ents) == 0 {
			// XXX: we're avoiding `Walk(..)` here
			// but it's hacky and gross. Our indexing should be better,
			// or we should just do the walk.
			var (
				parent p9.File
				name   = dirs[i]
			)
			if i == 0 {
				parent = root
			} else {
				parent = dirFiles[i-1]
			}
			if err := parent.UnlinkAt(name, 0); err != nil {
				return err
			}
		}
	}
	return nil
}

