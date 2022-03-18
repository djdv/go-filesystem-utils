package generic

import (
	"context"
	"sync"
)

// skipError is used as a sentinel value
// passed and inspected by functions to
// signify some element/phase should be skipped.
// Similar to `fs.SkipDir`.
type skipError string

const ErrSkip = skipError("skip")

func (e skipError) Error() string { return string(e) }

func CtxMerge[in any](ctx context.Context, sources ...<-chan in) <-chan in {
	var (
		mergedWg  sync.WaitGroup
		mergedCh  = make(chan in)
		mergeFrom = func(ch <-chan in) {
			defer mergedWg.Done()
			for value := range CtxRange(ctx, ch) {
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

func CtxRange[in any](ctx context.Context, input <-chan in) <-chan in {
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

func CtxTakeAndCancel[in any](ctx context.Context, cancel context.CancelFunc,
	count int, inputs <-chan in,
) <-chan in {
	relay := make(chan in, count)
	go func() {
		defer close(relay)
		defer cancel()
		for element := range CtxRange(ctx, inputs) {
			if count == 0 {
				return
			}
			select {
			case relay <- element:
			case <-ctx.Done():
				return
			}
			count--
		}
	}()
	return relay
}

func ForEachOrError[in any](ctx context.Context,
	input <-chan in, errs <-chan error, fn func(in) error,
) error {
	for input != nil ||
		errs != nil {
		select {
		case element, ok := <-input:
			if !ok {
				input = nil
				continue
			}
			if err := fn(element); err != nil {
				return err
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func ProcessResults[in, out any](ctx context.Context,
	input <-chan in, output chan out, errors chan error,
	phase func(in) (out, error),
) {
	for element := range CtxRange(ctx, input) {
		result, err := phase(element)
		if err != nil {
			if err == ErrSkip {
				continue
			}
			select {
			case errors <- err:
			case <-ctx.Done():
			}
			return
		}
		select {
		case output <- result:
		case <-ctx.Done():
			return
		}
	}
}
