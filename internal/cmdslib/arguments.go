package cmdslib

import (
	"context"
	"fmt"
	"reflect"

	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type (
	// Argument is the pairing of a Parameter with a Go variable.
	// The value is typically a pointer to a field within a Settings struct,
	// but any abstract reference value is allowed.
	Argument struct {
		parameters.Parameter
		ValueReference interface{}
	}
	Arguments <-chan Argument

	// SetFunc is provided a settings's Arguments to be set.
	// It should attempt to set the ValueReference,
	// by utilizing the Parameter to search some value store.
	// If SetFunc doesn't have a value for the Parameter,
	// it should relay the Argument to its output channel.
	// (So that subsequent SetFuncs may search their own value sources for it.)
	SetFunc func(context.Context, Arguments, ...TypeParser) (unsetArgs Arguments, _ <-chan error)

	// ParseFunc receives a string representation of the data value,
	// and should return a typed Go value of it.
	ParseFunc func(argument string) (value interface{}, _ error)

	// TypeParser is the binding of a type with its corresponding parser function.
	TypeParser struct {
		reflect.Type
		ParseFunc
	}
	typeParsers []TypeParser
)

func (parsers typeParsers) Index(typ reflect.Type) *TypeParser {
	for _, parser := range parsers {
		if parser.Type == typ {
			return &parser
		}
	}
	return nil
}

// TODO: outdated? check comment
/// Parse will populate `set` using values returned by each `SetFunc`.
// Value sources are queried in the same order they're provided.
// If a setting cannot be set by one source,
// it's reference is relayed to the next `SetFunc`.
func Parse[settings any, setIntf SettingsConstraint[settings]](ctx context.Context,
	setFuncs []SetFunc, parsers ...TypeParser,
) (*settings, error) {
	set := new(settings)
	unsetArgs, generatorErrs, err := ArgsFromSettings[settings, setIntf](ctx, set)
	if err != nil {
		return nil, err
	}

	const generatorChanCount = 1
	errChans := make([]<-chan error, 0,
		generatorChanCount+len(setFuncs),
	)
	errChans = append(errChans, generatorErrs)

	for _, setter := range setFuncs {
		var errChan <-chan error
		unsetArgs, errChan = setter(ctx, unsetArgs, parsers...)
		errChans = append(errChans, errChan)
	}

	var (
		errs  = CtxMerge(ctx, errChans...)
		drain = func(Argument) error { return nil }
	)
	if err := ForEachOrError(ctx, unsetArgs, errs, drain); err != nil {
		return nil, fmt.Errorf("Parse encountered an error: %w", err)
	}
	return set, ctx.Err()
}
