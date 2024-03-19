package generic_test

import (
	"reflect"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/generic"
)

func slice(t *testing.T) {
	t.Parallel()
	t.Run("clone", sliceClone)
}

func sliceClone(t *testing.T) {
	t.Parallel()
	const expectedCap = 8
	var (
		slice = make([]int, expectedCap, expectedCap*2)
		clone = generic.CloneSlice(slice)
	)
	if cloneCap := cap(clone); cloneCap != expectedCap {
		t.Errorf("slice capacity mismatched"+
			"\n\tgot: %d"+
			"\n\twant: %d",
			cloneCap, expectedCap)
	}
	slice = slice[:expectedCap]
	if !reflect.DeepEqual(slice, clone) {
		t.Errorf("slices not equal after clone"+
			"\n\tgot: %#v"+
			"\n\twant: %#v",
			slice, clone)
	}
}
