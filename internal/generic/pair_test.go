package generic_test

import (
	"context"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/generic"
)

func ctxPair(t *testing.T) {
	t.Parallel()
	t.Run("valid", testPairValid)
}

func testPairValid(t *testing.T) {
	t.Parallel()
	var (
		elements1 = []int{1, 2}
		elements2 = []string{"a", "b"}
		ch1       = buffAndClose(elements1...)
		ch2       = buffAndClose(elements2...)
	)

	var (
		index int
		ctx   = context.Background()
		pairs = generic.CtxBoth(ctx, ch1, ch2)
	)
	for pair := range pairs {
		var (
			t1 = pair.Left
			t2 = pair.Right
			e1 = elements1[index]
			e2 = elements2[index]
		)
		if t1 != e1 || t2 != e2 {
			expected := generic.Couple[int, string]{e1, e2}
			t.Errorf("pair values don't match expected data"+
				"\n\tgot: %#v"+
				"\n\twant: %#v",
				pair, expected)
		}
		index++
	}
}
