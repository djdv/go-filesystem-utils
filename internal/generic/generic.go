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

// CtxPair receives a value from both channels
// and relays them as a single Couple,
// until either channel closes or the context is done.
func CtxPair[left, right any](ctx context.Context,
	leftIn <-chan left, rightIn <-chan right,
) <-chan Couple[left, right] {
	pairs := make(chan Couple[left, right], max(cap(leftIn), cap(rightIn)))
	go func() {
		defer close(pairs)
		for {
			pair, ok := maybeReceivePair(ctx, leftIn, rightIn)
			if !ok {
				return
			}
			select {
			case pairs <- pair:
			case <-ctx.Done():
				return
			}
		}
	}()
	return pairs
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func maybeReceivePair[left, right any](ctx context.Context,
	leftIn <-chan left, rightIn <-chan right,
) (pair Couple[left, right], ok bool) {
	{
		l, r, setLeft, ok := receiveLeftOrRight(ctx, leftIn, rightIn)
		if !ok {
			return pair, ok
		}
		if setLeft {
			leftIn = nil
		} else {
			rightIn = nil
		}
		assignLeftOrRight(&pair, l, r, setLeft)
	}
	l, r, setLeft, ok := receiveLeftOrRight(ctx, leftIn, rightIn)
	assignLeftOrRight(&pair, l, r, setLeft)
	return pair, ok
}

func receiveLeftOrRight[leftType, rightType any](ctx context.Context,
	leftIn <-chan leftType, rightIn <-chan rightType,
) (left leftType, right rightType, gotLeft, ok bool) {
	select {
	case left, ok = <-leftIn:
		gotLeft = true
	case right, ok = <-rightIn:
	case <-ctx.Done():
		ok = false
	}
	return
}

func assignLeftOrRight[left, right any](
	pair *Couple[left, right],
	l left, r right,
	setLeft bool,
) {
	if setLeft {
		pair.Left = l
	} else {
		pair.Right = r
	}
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
