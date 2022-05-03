package runtime

import (
	"context"
	"fmt"
	"reflect"

	"github.com/djdv/go-filesystem-utils/internal/generic"
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
)

func maybeGetParser(typ reflect.Type, parsers ...TypeParser) *TypeParser {
	for _, parser := range parsers {
		if parser.Type == typ {
			return &parser
		}
	}
	return nil
}

// TODO: outdated? check comment
// / Parse will populate `set` using values returned by each `SetFunc`.
// Value sources are queried in the same order they're provided.
// If a setting cannot be set by one source,
// it's reference is relayed to the next `SetFunc`.
func Parse[setIntf SettingsConstraint[set], set any](ctx context.Context,
	setFuncs []SetFunc, parsers ...TypeParser,
) (*set, error) {
	settingsPointer := new(set)
	unsetArgs, generatorErrs, err := argsFromSettings[setIntf](ctx, settingsPointer)
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
		errs  = generic.CtxMerge(ctx, errChans...)
		drain = func(Argument) error { return nil }
	)
	if err := generic.ForEachOrError(ctx, unsetArgs, errs, drain); err != nil {
		return nil, fmt.Errorf("Parse encountered an error: %w", err)
	}
	return settingsPointer, ctx.Err()
}

func Assign(arg Argument, value any) error {
	targetValue := reflect.ValueOf(arg.ValueReference).Elem()
	if !targetValue.CanSet() {
		return fmt.Errorf("%w: `reflect.Value.CanSet` returned false for argument reference",
			ErrUnassignable,
		)
	}

	var (
		targetType   = targetValue.Type()
		reflectValue = reflect.ValueOf(value)
		reflectType  = reflectValue.Type()
	)
	if !targetType.AssignableTo(reflectType) {
		err := fmt.Errorf("%w: `%#v` to %v",
			ErrUnassignable, value, targetType,
		)
		return err
	}
	targetValue.Set(reflectValue)
	return nil
}
