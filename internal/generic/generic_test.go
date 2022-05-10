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
		ctx        = context.Background()
		leftSlice  = []int{1, 2}
		rightSlice = []string{"a", "b"}
		leftIn     = buffAndClose(leftSlice...)
		rightIn    = buffAndClose(rightSlice...)
		tuples     = generic.CtxEither(ctx, leftIn, rightIn)
		gotLeft    = []int{}
		gotRight   = []string{}
	)

	for tuple := range tuples {
		var (
			t1 = tuple.Left
			t2 = tuple.Right
		)
		if t1 != 0 {
			gotLeft = append(gotLeft, t1)
		}
		if t2 != "" {
			gotRight = append(gotRight, t2)
		}
	}
	if !reflect.DeepEqual(leftSlice, gotLeft) ||
		!reflect.DeepEqual(rightSlice, gotRight) {
		t.Errorf("Tuple did not match expected data"+
			"\n\tgot: %#v %#v"+
			"\n\twant: %#v %#v",
			gotLeft, gotRight, leftSlice, rightSlice)
	}
}

func ctxEitherInvalid(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tuples := generic.CtxEither(ctx, buffAndClose(1, 2), buffAndClose("a", "b"))
	for tuple := range tuples {
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
