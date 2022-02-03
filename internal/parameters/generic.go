package parameters

import (
	"context"
	"sync"
)

// skipError is used as a sentinel value
// passed and inspected by functions to
// signify some element/phase should be skipped.
// Similar to `fs.SkipDir`.
type skipError string

const errSkip = skipError("skip")

func (e skipError) Error() string { return string(e) }

func ctxMerge[in any](ctx context.Context, sources ...<-chan in) <-chan in {
	var (
		mergedWg  sync.WaitGroup
		mergedCh  = make(chan in)
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

func ctxTakeAndCancel[in any](ctx context.Context, cancel context.CancelFunc,
	count int, inputs <-chan in,
) <-chan in {
	relay := make(chan in, count)
	go func() {
		defer close(relay)
		defer cancel()
		for element := range ctxRange(ctx, inputs) {
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

func forEachOrError[in any](ctx context.Context,
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

func processResults[in, out any](ctx context.Context,
	input <-chan in, output chan out, errors chan error,
	phase func(in) (out, error),
) {
	for element := range ctxRange(ctx, input) {
		result, err := phase(element)
		if err != nil {
			if err == errSkip {
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
