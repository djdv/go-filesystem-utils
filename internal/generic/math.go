package generic

import "golang.org/x/exp/constraints"

// TODO: is this in the standard somewhere yet?
func Max[T constraints.Ordered](x, y T) T {
	if x > y {
		return x
	}
	return y
}

// TODO: is this in the standard somewhere yet?
func Min[T constraints.Ordered](x, y T) T {
	if x < y {
		return x
	}
	return y
}
