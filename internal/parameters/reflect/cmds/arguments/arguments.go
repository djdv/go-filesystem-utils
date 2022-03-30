package arguments

import (
	"context"
	"fmt"

	. "github.com/djdv/go-filesystem-utils/internal/generic"
	parameters "github.com/djdv/go-filesystem-utils/internal/parameters/reflect"
)

type (
	// SetFunc is provided a Settings's Arguments to be set.
	// It should attempt to set the ValueReference,
	// by utilizing the Parameter to search some value store.
	// If SetFunc doesn't have a value for the Parameter,
	// it should relay the Argument in its output channel.
	// (So that subsequent SetFuncs may search their own value sources for it.)
	SetFunc func(context.Context, parameters.Arguments, ...parameters.TypeParser) (unsetArgs parameters.Arguments, _ <-chan error)

	errCh = <-chan error
)

// TODO: outdated? check comment
/// Parse will populate `set` using values returned by each `SetFunc`.
// Value sources are queried in the same order they're provided.
// If a setting cannot be set by one source,
// it's reference is relayed to the next `SetFunc`.
func Parse[settings any](ctx context.Context, setFuncs []SetFunc, parsers ...parameters.TypeParser) (*settings, error) {
	var (
		set            = new(settings)
		subCtx, cancel = context.WithCancel(ctx)
	)
	defer cancel()

	unsetArgs, generatorErrs, err := parameters.ArgsFromSettings(subCtx, set)
	if err != nil {
		return nil, err
	}

	const generatorChanCount = 1
	errChans := make([]errCh, 0, len(setFuncs)+generatorChanCount)
	errChans = append(errChans, generatorErrs)

	for _, setter := range setFuncs {
		var errCh errCh
		unsetArgs, errCh = setter(subCtx, unsetArgs, parsers...)
		errChans = append(errChans, errCh)
	}

	var (
		errs  = CtxMerge(subCtx, errChans...)
		drain = func(parameters.Argument) error { return nil }
	)
	if err := ForEachOrError(subCtx, unsetArgs, errs, drain); err != nil {
		return nil, fmt.Errorf("Parse encountered an error: %w", err)
	}
	return set, subCtx.Err()
}
