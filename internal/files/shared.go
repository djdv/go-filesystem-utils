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

type walker[F p9.File] interface {
	fidOpened() bool
	clone(withQid bool) ([]p9.QID, F)
	parent() p9.File // TODO: we only need *parent + parent.QID
	files() fileTable
}

func walk[F p9.File](file walker[F], names ...string) ([]p9.QID, p9.File, error) {
	if file.fidOpened() {
		return nil, nil, perrors.EINVAL // TODO: [spec] correct evalue?
	}
	switch nameCount := len(names); nameCount {
	case 0:
		const withQID = false
		_, nf := file.clone(withQID)
		return nil, nf, nil
	case 1:
		switch names[0] {
		case parentWName:
			if parent := file.parent(); parent != nil {
				qid, _, _, err := parent.GetAttr(p9.AttrMask{})
				return []p9.QID{qid}, parent, err
			}
			fallthrough // Root's `..` is itself.
		case selfWName:
			const withQID = true
			qids, nf := file.clone(withQID)
			return qids, nf, nil
		}
	}
	if entries := file.files(); entries != nil {
		return walkRecur(entries, names...)
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

func mkdirMask(permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.SetAttrMask, p9.SetAttr) {
	return attrToSetAttr(&p9.Attr{
		Mode: (permissions &^ S_LINMSK) & S_IRWXA,
		UID:  uid,
		GID:  gid,
	})
}

func mknodMask(permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.SetAttrMask, p9.SetAttr) {
	return attrToSetAttr(&p9.Attr{
		Mode: permissions &^ S_LINMSK,
		UID:  uid,
		GID:  gid,
	})
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
