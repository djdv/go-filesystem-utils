package generic_test

import (
	"context"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/generic"
)

func ctxPair(t *testing.T) {
	t.Parallel()
	t.Run("valid", testPairValid)
	t.Run("invalid", testPairInvalid)
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
		index       int
		ctx         = context.Background()
		pairs, errs = generic.CtxPair(ctx, ch1, ch2)
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
	for err := range errs {
		t.Error(err)
	}
}

func testPairInvalid(t *testing.T) {
	t.Parallel()
	t.Run("left close", leftClose)
	t.Run("right close", rightClose)
}

func leftClose(t *testing.T) {
	t.Parallel()
	var (
		lastErr error

		ch1 = buffAndClose(1)
		ch2 = buffAndClose("a", "b")

		ctx         = context.Background()
		pairs, errs = generic.CtxPair(ctx, ch1, ch2)
	)
	for pairs != nil || errs != nil {
		select {
		case _, ok := <-pairs:
			if !ok {
				pairs = nil
				continue
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			lastErr = err
		}
	}
	if lastErr == nil {
		t.Error("expected error but received none (left channel closed early)")
	}
}

func rightClose(t *testing.T) {
	t.Parallel()
	var (
		lastErr error

		ch1 = buffAndClose(1, 2)
		ch2 = buffAndClose("a")

		ctx         = context.Background()
		pairs, errs = generic.CtxPair(ctx, ch1, ch2)
	)
	for pairs != nil || errs != nil {
		select {
		case _, ok := <-pairs:
			if !ok {
				pairs = nil
				continue
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			lastErr = err
		}
	}
	if lastErr == nil {
		t.Error("expected error but received none (right channel closed early)")
	}
}
