// Package generic provides a set of type agnostic helpers.
package generic

import (
	"context"
	"fmt"
	"sync"
)

// Couple wraps 2 values (of any type)
// under the generic names `Left` and `Right`.
type Couple[left, right any] struct {
	Left  left
	Right right
}

// CtxEither receives from both channels,
// relaying either the left or right type,
// until both channels are closed or the context is done.
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

// CtxPair receives values from both channels and relays them as a Couple
// until both channels are closed or the context is done.
// An error is sent if one channel is closed while the other is still being sent values.
func CtxPair[t1, t2 any](ctx context.Context,
	leftIn <-chan t1, rightIn <-chan t2,
) (<-chan Couple[t1, t2], <-chan error) {
	var (
		tuples = make(chan Couple[t1, t2], cap(leftIn))
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
				case tuples <- Couple[t1, t2]{Left: left, Right: right}:
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

func totalBuff[in any](inputs []<-chan in) (total int) {
	for _, ch := range inputs {
		total += cap(ch)
	}
	return
}

func CtxMerge[in any](ctx context.Context, sources ...<-chan in) <-chan in {
	var (
		mergedWg  sync.WaitGroup
		mergedCh  = make(chan in, totalBuff(sources))
		mergeFrom = func(ch <-chan in) {
			defer mergedWg.Done()
			for value := range ctxRange(ctx, ch) {
				select {
				case mergedCh <- value:
				case <-ctx.Done():
					return
				}
			}
		}
	)

	mergedWg.Add(len(sources))
	for _, source := range sources {
		go mergeFrom(source)
	}

	go func() { mergedWg.Wait(); close(mergedCh) }()

	return mergedCh
}
