package generic

// CloneSlice is analogous to [slices.Clone]
// except that is uses `copy` instead of `append`.
// I.e. the cloned slices has cap == len(slice).
func CloneSlice[T any](slice []T) []T {
	clone := make([]T, len(slice))
	copy(clone, slice)
	return clone
}

// UpsertSlice returns a function which
// appends (on the first call)
// or updates (on subsequent calls)
// an element in the slice being pointed to.
func UpsertSlice[S ~[]T, T any](slicePtr *S) func(T) {
	var (
		set   bool
		index int
	)
	return func(element T) {
		if set {
			(*slicePtr)[index] = element
		} else {
			index = len(*slicePtr)
			*slicePtr = append(*slicePtr, element)
			set = true
		}
	}
}
