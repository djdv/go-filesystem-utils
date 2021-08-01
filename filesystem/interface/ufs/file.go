package ufs

import (
	"github.com/ipfs/go-unixfs/mod"
)

// dagref extends `DagModifier`
// that is intended to be shared by multiple callers
// who utilize the lock and adjust the reference count accordingly
// dagRefs are intended to be managed internally by keyfs.
type dagRef struct {
	*mod.DagModifier
	modifiedCallback ModifiedFunc
}

func (dr *dagRef) Truncate(size uint64) error {
	err := dr.DagModifier.Truncate(int64(size))
	if err != nil {
		return err
	}

	if dr.modifiedCallback != nil {
		node, err := dr.DagModifier.GetNode()
		if err != nil {
			return err
		}
		return dr.modifiedCallback(node)
	}

	return nil
}

func (dr *dagRef) Write(buff []byte) (int, error) {
	wroteBytes, err := dr.DagModifier.Write(buff)
	if err != nil {
		return wroteBytes, err
	}

	if wroteBytes != 0 && dr.modifiedCallback != nil {
		node, err := dr.DagModifier.GetNode()
		if err != nil {
			return wroteBytes, err
		}
		return wroteBytes, dr.modifiedCallback(node)
	}

	return wroteBytes, err
}

func (dr *dagRef) Close() error {
	if err := dr.DagModifier.Sync(); err != nil {
		return err
	}

	if dr.modifiedCallback != nil {
		node, err := dr.DagModifier.GetNode()
		if err != nil {
			return err
		}
		return dr.modifiedCallback(node)
	}

	return nil
}
