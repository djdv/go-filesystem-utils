package errors

import (
	"context"
	"fmt"
	"sync"
)

// TODO: names and docs

// TODO: placeholder name and value - we use it to implement unwinding request transactions
// (if any instance fails to open, previously opened instances close and respond with this error)
var Unwound = fmt.Errorf("instance was requested to close")

type Stream = <-chan error

func Merge(errorStreams ...Stream) Stream {
	switch len(errorStreams) {
	case 0:
		empty := make(chan error)
		close(empty)
		return empty
	case 1:
		return errorStreams[0]
	}
	mergedStream := make(chan error)

	var wg sync.WaitGroup
	mergeFrom := func(errors Stream) {
		for err := range errors {
			mergedStream <- err
		}
		wg.Done()
	}

	wg.Add(len(errorStreams))
	for _, Errors := range errorStreams {
		go mergeFrom(Errors)
	}

	go func() { wg.Wait(); close(mergedStream) }()
	return mergedStream
}

func Splice(ctx context.Context, errorStreams <-chan Stream) Stream {
	streamPlex := make(chan error, len(errorStreams))

	var wg sync.WaitGroup
	sourceFrom := func(errors Stream) {
		defer wg.Done()
		for err := range errors {
			select {
			case streamPlex <- err:
			case <-ctx.Done():
				return
			}
		}
	}

	go func() {
		for errors := range errorStreams {
			wg.Add(1)
			go sourceFrom(errors)
		}
		wg.Wait()
		close(streamPlex)
	}()

	return streamPlex
}

func Accumulate(ctx context.Context, errors Stream) (errs []error) {
	for {
		select {
		case err, ok := <-errors:
			if !ok {
				return
			}
			errs = append(errs, err)
		case <-ctx.Done():
			return
		}
	}
}
