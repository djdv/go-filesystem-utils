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
	t.Run("CtxBoth", ctxBoth)
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
	)
	for leftOrRight := range generic.CtxEither(ctx, leftIn, rightIn) {
		if left := leftOrRight.Left; left != 0 {
			gotLeft = append(gotLeft, left)
			continue
		}
		gotRight = append(gotRight, leftOrRight.Right)
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
	t.Run("canceled", canceledEither)
}

func canceledEither(t *testing.T) {
	t.Parallel()
	t.Run("before send", canceledEitherBeforeSend)
	t.Run("after send", canceledEitherAfterSend)
}

func canceledEitherBeforeSend(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// CtxEither starts canceled, and never receives any values from us.
	// If this receive doesn't complete, the test will fail via time out.
	<-generic.CtxEither(ctx, make(chan struct{}), (chan struct{})(nil))
}

func canceledEitherAfterSend(t *testing.T) {
	t.Parallel()
	var (
		singleChan  = make(chan struct{})
		ctx, cancel = context.WithCancel(context.Background())
	)
	go func() { singleChan <- struct{}{}; cancel() }()
	// NOTE: Because of the runtime's randomness during `select`,
	// the channel may send its value or be closed at this point.
	<-generic.CtxEither(ctx, singleChan, (chan struct{})(nil))
}

func buffAndClose[in any](elements ...in) <-chan in {
	out := make(chan in, len(elements))
	for _, elem := range elements {
		out <- elem
	}
	close(out)
	return out
}
