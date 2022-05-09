/*
Package generic provides a set of type agnostic helpers.
*/
package generic

import (
	"context"
	"fmt"
)

// Tuple wraps 2 values of any type
// allowing them to be generically addressed by names `Left` and `Right`.
// Conventionally: `someFunc(left, right) Tuple{leftType, rightType} { ...`.
type Tuple[t1, t2 any] struct {
	Left  t1
	Right t2
}

// CtxPair receives values from both channels and relays them as a Tuple
// until both channels are closed or the context is done.
// An error is sent if one channel is closed while the other is still being sent values.
func CtxPair[t1, t2 any](ctx context.Context,
	leftIn <-chan t1, rightIn <-chan t2,
) (<-chan Tuple[t1, t2], <-chan error) {
	var (
		tuples = make(chan Tuple[t1, t2], cap(leftIn))
		errs   = make(chan error)
	)
	go func() {
		defer close(tuples)
		defer close(errs)
		ctxSendErr := func(err error) {
			select {
			case errs <- err:
			case <-ctx.Done():
			}
		}
		for left := range ctxRange(ctx, leftIn) {
			select {
			case right, ok := <-rightIn:
				if !ok {
					err := fmt.Errorf("t2 closed with t1 open")
					ctxSendErr(err)
					return
				}
				select {
				case tuples <- Tuple[t1, t2]{Left: left, Right: right}:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
		select {
		case _, ok := <-rightIn:
			if ok {
				err := fmt.Errorf("t1 closed with t2 open")
				ctxSendErr(err)
			}
		case <-ctx.Done():
		}
	}()
	return tuples, errs
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
			select {
			case left, ok := <-leftIn:
				if !ok {
					leftIn = nil
					continue
				}
				select {
				case tuples <- Tuple[t1, t2]{Left: left}:
				case <-ctx.Done():
					return
				}
			case right, ok := <-rightIn:
				if !ok {
					rightIn = nil
					continue
				}
				select {
				case tuples <- Tuple[t1, t2]{Right: right}:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return tuples
}

// ctxRange relays values received from `input`
// until it is closed or the context is done.
// Intended to be used as a range expression
// `for element := range ctxRange(ctx, elementChan)`
func ctxRange[in any](ctx context.Context, input <-chan in) <-chan in {
	relay := make(chan in, cap(input))
	go func() {
		defer close(relay)
		for {
			select {
			case element, ok := <-input:
				if !ok {
					return
				}
				select {
				case relay <- element:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return relay
}
