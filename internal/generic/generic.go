// Package generic provides a set of type agnostic helpers.
package generic

import "context"

// Couple wraps 2 values of any type
// allowing them to be generically addressed by names `Left` and `Right`.
// Conventionally: `someFunc(left, right) Couple{leftType, rightType} { ...`.
type Couple[left, right any] struct {
	Left  left
	Right right
}

// CtxEither receives from both channels,
// relaying either the left or right type,
// until both channels are closed, or the context is done.
func CtxEither[left, right any](ctx context.Context,
	leftIn <-chan left, rightIn <-chan right,
) <-chan Couple[left, right] {
	eithers := make(chan Couple[left, right], cap(leftIn)+cap(rightIn))
	go func() {
		defer close(eithers)
		for leftIn != nil ||
			rightIn != nil {
			var (
				either Couple[left, right]
				isOpen bool
			)
			select {
			case either.Left, isOpen = <-leftIn:
				if !isOpen {
					leftIn = nil
					continue
				}
			case either.Right, isOpen = <-rightIn:
				if !isOpen {
					rightIn = nil
					continue
				}
			case <-ctx.Done():
				return
			}
			select {
			case eithers <- either:
			case <-ctx.Done():
				return
			}
		}
	}()
	return eithers
}
