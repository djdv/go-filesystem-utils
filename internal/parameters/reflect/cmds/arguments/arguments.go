package arguments

import (
	"context"
	"fmt"

	. "github.com/djdv/go-filesystem-utils/internal/generic"
	goparams "github.com/djdv/go-filesystem-utils/internal/parameters/reflect"
)

type (
	// SetFunc is provided a Settings's Arguments to be set.
	// It should attempt to set the ValueReference,
	// by utilizing the Parameter to search some value store.
	// If SetFunc doesn't have a value for the Parameter,
	// it should relay the Argument in its output channel.
	// (So that subsequent SetFuncs may search their own value sources for it.)
	SetFunc func(context.Context, goparams.Arguments, ...goparams.TypeParser) (unsetArgs goparams.Arguments, _ <-chan error)

	errCh = <-chan error
)

// TODO: outdated? check comment
/// Parse will populate `set` using values returned by each `SetFunc`.
// Value sources are queried in the same order they're provided.
// If a setting cannot be set by one source,
// it's reference is relayed to the next `SetFunc`.
func Parse[settings any, setIntf goparams.SettingsConstraint[settings]](ctx context.Context,
	setFuncs []SetFunc, parsers ...goparams.TypeParser,
) (*settings, error) {
	var (
		set          = new(settings)
		intf setIntf = set
	)

	unsetArgs, generatorErrs, err := goparams.ArgsFromSettings(ctx, intf)
	if err != nil {
		return nil, err
	}

	const generatorChanCount = 1
	errChans := make([]errCh, 0,
		generatorChanCount+len(setFuncs),
	)
	errChans = append(errChans, generatorErrs)

	for _, setter := range setFuncs {
		var errCh errCh
		unsetArgs, errCh = setter(ctx, unsetArgs, parsers...)
		errChans = append(errChans, errCh)
	}

	var (
		errs  = CtxMerge(ctx, errChans...)
		drain = func(goparams.Argument) error { return nil }
	)
	if err := ForEachOrError(ctx, unsetArgs, errs, drain); err != nil {
		return nil, fmt.Errorf("Parse encountered an error: %w", err)
	}
	return set, ctx.Err()
}
