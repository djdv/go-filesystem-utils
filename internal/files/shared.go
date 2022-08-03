package files

import "golang.org/x/exp/constraints"

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
