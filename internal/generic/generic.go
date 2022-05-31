// Package generic provides a set of type agnostic helpers.
package generic

import (
	"context"
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

// CtxBoth receives a single value from both channels
// and binds them as a Couple,
// until either channel closes or the context is done.
func CtxBoth[both Couple[left, right], left, right any](ctx context.Context,
	leftIn <-chan left, rightIn <-chan right,
) <-chan both {
	boths := make(chan both, max(cap(leftIn), cap(rightIn)))
	go func() {
		defer close(boths)
		for {
			var (
				leftValue          left
				rightValue         right
				leftChan           = leftIn
				rightChan          = rightIn
				receiveLeftOrRight = func() (ok bool) {
					select {
					case leftValue, ok = <-leftChan:
						leftChan = nil
					case rightValue, ok = <-rightChan:
						rightChan = nil
					case <-ctx.Done():
						ok = false
					}
					return
				}
			)
			if !receiveLeftOrRight() {
				return
			}
			if !receiveLeftOrRight() {
				return
			}
			select {
			case boths <- both{Left: leftValue, Right: rightValue}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return boths
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
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
		mergeFrom = func(source <-chan in) {
			defer mergedWg.Done()
			for {
				select {
				case value, ok := <-source:
					if !ok {
						return
					}
					select {
					case mergedCh <- value:
					case <-ctx.Done():
						return
					}
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
