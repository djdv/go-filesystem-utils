// Package generic provides a set of type agnostic helpers.
package generic

import "context"

// Tuple wraps 2 values of any type
// allowing them to be generically addressed by names `Left` and `Right`.
// Conventionally: `someFunc(left, right) Tuple{leftType, rightType} { ...`.
type Tuple[t1, t2 any] struct {
	Left  t1
	Right t2
}

// CtxEither receives from both channels,
// relaying either the left or right type,
// until both channels are closed, or the context is done.
func CtxEither[t1, t2 any](ctx context.Context,
	leftIn <-chan t1, rightIn <-chan t2,
) <-chan Tuple[t1, t2] {
	tuples := make(chan Tuple[t1, t2], cap(leftIn)+cap(rightIn))
	go func() {
		defer close(tuples)
		for leftIn != nil ||
			rightIn != nil {
			var tuple Tuple[t1, t2]
			select {
			case left, ok := <-leftIn:
				if !ok {
					leftIn = nil
					continue
				}
				tuple.Left = left
			case right, ok := <-rightIn:
				if !ok {
					rightIn = nil
					continue
				}
				tuple.Right = right
			case <-ctx.Done():
				return
			}
			select {
			case tuples <- tuple:
			case <-ctx.Done():
				return
			}
		}
	}()
	return tuples
}
