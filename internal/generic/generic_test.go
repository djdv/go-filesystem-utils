package generic_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/generic"
)

func TestGeneric(t *testing.T) {
	t.Parallel()
	t.Run("CtxEither", ctxEither)
}

func ctxEither(t *testing.T) {
	t.Parallel()
	t.Run("valid", ctxEitherValid)
	t.Run("invalid", ctxEitherInvalid)
}

func ctxEitherValid(t *testing.T) {
	t.Parallel()
	var (
		wantLeft  = []int{1, 2}
		wantRight = []string{"a", "b"}
		gotLeft   = make([]int, 0, len(wantLeft))
		gotRight  = make([]string, 0, len(wantRight))

		ctx     = context.Background()
		leftIn  = buffAndClose(wantLeft...)
		rightIn = buffAndClose(wantRight...)
		pairs   = generic.CtxEither(ctx, leftIn, rightIn)
	)
	for pair := range pairs {
		if left := pair.Left; left != 0 {
			gotLeft = append(gotLeft, left)
			continue
		}
		if right := pair.Right; right != "" {
			gotRight = append(gotRight, right)
		}
	}
	var (
		leftMatches  = reflect.DeepEqual(wantLeft, gotLeft)
		rightMatches = reflect.DeepEqual(wantRight, gotRight)
		mismatch     = !leftMatches || !rightMatches
	)
	if mismatch {
		t.Errorf("Tuple did not match expected data"+
			"\n\tgot: %#v %#v"+
			"\n\twant: %#v %#v",
			gotLeft, gotRight,
			wantLeft, wantRight,
		)
	}
}

func ctxEitherInvalid(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range generic.CtxEither(ctx,
		buffAndClose(1, 2),
		buffAndClose("a", "b"),
	) {
	}
}

func buffAndClose[in any](elements ...in) <-chan in {
	out := make(chan in, len(elements))
	for _, elem := range elements {
		out <- elem
	}
	close(out)
	return out
}
