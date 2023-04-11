package generic

// CloneSlice is analogous to [slices.Clone]
// except that is uses `copy` instead of `append`.
// I.e. the cloned slices has cap == len(slice).
func CloneSlice[T any](slice []T) []T {
	clone := make([]T, len(slice))
	copy(clone, slice)
	return clone
}

// CompactSlice will return either
// the input slice, or a copy of it
// with a cap == len(slice).
func CompactSlice[T any](slice []T) []T {
	if len(slice) == cap(slice) {
		return slice
	}
	return CloneSlice(slice)
}
