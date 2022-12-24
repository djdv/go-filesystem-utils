package p9

import (
	"log"

	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

const (
	selfWName   = "."
	parentWName = ".."
)

type (
	// TODO: namen
	placeholderName interface {
		fidOpened() bool
	}
	childWalker interface {
		parent() p9.File
	}
	fileWalker[F p9.File] interface {
		placeholderName
		clone(withQid bool) ([]p9.QID, F, error)
	}
	dirWalker[F p9.File] interface {
		fileWalker[F]
		load(name string) (p9.File, bool)
	}
)

func walk[F p9.File](file fileWalker[F], names ...string) ([]p9.QID, p9.File, error) {
	if file.fidOpened() {
		log.Printf("already opened: %p", file)
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
			if child, ok := file.(childWalker); ok {
				return child.parent().Walk([]string{selfWName})
			}
			fallthrough
		case selfWName:
			return file.clone(withQID)
		}
	}
	if dir, ok := file.(dirWalker[F]); ok {
		return walkRecur[dirWalker[F]](dir, names...)
	}
	return nil, nil, perrors.ENOTDIR
}

func walkRecur[D dirWalker[F], F p9.File](dir dirWalker[F], names ...string) ([]p9.QID, p9.File, error) {
	file, ok := dir.load(names[0])
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
