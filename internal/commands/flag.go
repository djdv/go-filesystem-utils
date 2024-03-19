package commands

import (
	"flag"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/p9/p9"
)

type (
	// optionsReference is any
	// `*optionSlice`.
	optionsReference[
		OS optionSlice[OT, T],
		OT generic.OptionFunc[T],
		T any,
	] interface {
		*OS
	}
	// optionSlice is any
	// `[]generic.OptionFunc`.
	optionSlice[
		OT generic.OptionFunc[T],
		T any,
	] interface {
		~[]OT
	}
	// genericFuncValue extends [flag.funcValue]
	// to add [command.ValueNamer] support.
	// (Because standard uses internal types
	// in a way we can't access;
	// see: [flag.UnquoteUsage]'s implementation.)
	genericFuncValue[T any] func(string) error
	// genericBoolFuncValue implements [flag.Value]'s
	// `isBoolFlag` extension.
	genericBoolFuncValue[T any] struct{ genericFuncValue[T] }
	// flagSetFunc is primarily used to implement
	// a [flag.Vale]'s Set method.
	flagSetFunc func(string) error
)

func (gf genericFuncValue[T]) Set(s string) error {
	if s == "" {
		// The [FlagSet.Parse] method already prefixes
		// the flag name and value, we just need
		// to provide the reason parsing failed.
		return generic.ConstError("empty value")
	}
	return gf(s)
}
func (gf genericFuncValue[T]) String() string { return "" }
func (gf genericFuncValue[T]) Name() string {
	name := reflect.TypeOf([0]T{}).Elem().String()
	if name == "bool" {
		return ""
	}
	if index := strings.LastIndexByte(name, '.'); index != -1 {
		name = name[index+1:] // Remove [QualifiedIdent] prefix.
	}
	return strings.ToLower(name)
}

func (gb genericBoolFuncValue[T]) IsBoolFlag() bool { return true }

// noParse returns it's argument.
// Useful as a `parseFunc` for [insertSlice], et al.
// when no processing is actually needed.
func noParse(argument string) (string, error) {
	return argument, nil
}

func parseID[id uint32 | p9.UID | p9.GID](arg string) (id, error) {
	if arg == "nobody" {
		var value id
		switch any(value).(type) {
		case p9.UID:
			value = id(p9.NoUID)
		case p9.GID:
			value = id(p9.NoGID)
		case uint32:
			value = id(math.MaxUint32)
		}
		return value, nil
	}
	const idSize = 32
	num, err := strconv.ParseUint(arg, 0, idSize)
	if err != nil {
		return 0, err
	}
	return id(num), nil
}

func idString[id uint32 | p9.UID | p9.GID](who id) string {
	const nobody = "nobody"
	switch typed := any(who).(type) {
	case p9.UID:
		if typed == p9.NoUID {
			return nobody
		}
	case p9.GID:
		if typed == p9.NoGID {
			return nobody
		}
	case uint32:
		if typed == math.MaxUint32 {
			return nobody
		}
	}
	return strconv.Itoa(int(who))
}

func setFlag[T any](
	flagSet *flag.FlagSet, name, usage string,
	setFn flagSetFunc,
) {
	value := newFlagValue[T](setFn)
	flagSet.Var(value, name, usage)
}

func setFlagOnce[T any](
	flagSet *flag.FlagSet, name, usage string,
	setFn flagSetFunc,
) {
	var (
		called         bool
		parseFnWrapped = func(argument string) error {
			if called {
				// NOTE: the flag package already
				// prints the name, etc.
				return generic.ConstError(
					"flag may not be provided multiple times",
				)
			}
			called = true
			return setFn(argument)
		}
	)
	setFlag[T](flagSet, name, usage, parseFnWrapped)
}

func setValue[
	flagT any,
	parseFunc func(argument string) (flagT, error),
	assignFunc func(value flagT),
](
	flagSet *flag.FlagSet, name, usage string,
	parseFn parseFunc, assignFn assignFunc,
) {
	var (
		parseFnWrapped = func(argument string) error {
			value, err := parseFn(argument)
			if err != nil {
				return err
			}
			assignFn(value)
			return nil
		}
		value = newFlagValue[flagT](parseFnWrapped)
	)
	flagSet.Var(value, name, usage)
}

func setValueOnce[
	flagT any,
	parseFunc func(argument string) (flagT, error),
	assignFunc func(value flagT),
](
	flagSet *flag.FlagSet, name, usage string,
	parseFn parseFunc, assignFn assignFunc,
) {
	var (
		previous       *flagT
		parseFnWrapped = func(argument string) (flagT, error) {
			if previous != nil {
				value := *previous
				return value, flagAlreadyProvidedErr(value)
			}
			value, err := parseFn(argument)
			if err == nil {
				previous = &value
			}
			return value, err
		}
	)
	setValue(flagSet, name, usage, parseFnWrapped, assignFn)
}

// TODO: del
// insertSlice will insert values returned from `transformFn`
// into the `elements` slice.
// `parseFn` is typically a parse function from [strconv],
// such as [strconv.ParseBool].
func insertSlice[
	flagT any,
	slice ~[]elementT, elementT any,
	parseFunc func(argument string) (flagT, error),
	transformFunc func(value flagT) elementT,
](
	flagSet *flag.FlagSet, name, usage string,
	elements *slice, parseFn parseFunc, transformFn transformFunc,
) {
	assignFn := func(value flagT) {
		element := transformFn(value)
		*elements = append(*elements, element)
	}
	setValue(flagSet, name, usage, parseFn, assignFn)
}

// insertSliceOnce is like [insertSlice]
// but disallows the flag to be provided multiple times.
func insertSliceOnce[
	flagT any,
	slice ~[]element, element any,
	parseFunc func(argument string) (flagT, error),
	transformFunc func(value flagT) element,
](
	flagSet *flag.FlagSet, name, usage string,
	elements *slice, parseFn parseFunc, transformFn transformFunc,
) {
	var (
		previous     any
		parseWrapped = func(argument string) (flagT, error) {
			if previous != nil {
				var zero flagT
				return zero, flagAlreadyProvidedErr(previous)
			}
			value, err := parseFn(argument)
			previous = value
			return value, err
		}
		assignFn = func(value flagT) {
			element := transformFn(value)
			*elements = append(*elements, element)
		}
	)
	setValue(flagSet, name, usage, parseWrapped, assignFn)
}

// upsertSlice will insert or update the `elements` slice
// with the value returned from `transformFn`.
// `parseFn` is typically a parse function from [strconv],
// such as [strconv.ParseBool].
func upsertSlice[
	flagT any,
	slice ~[]elementT, elementT any,
	parseFunc func(argument string) (flagT, error),
	transformFunc func(value flagT) elementT,
](
	flagSet *flag.FlagSet, name, usage string,
	elements *slice, parseFn parseFunc, transformFn transformFunc,
) {
	var (
		upsertFn = generic.UpsertSlice(elements)
		assignFn = func(value flagT) {
			element := transformFn(value)
			upsertFn(element)
		}
	)
	setValue(flagSet, name, usage, parseFn, assignFn)
}

// newFlagValue wraps `parseFn` to implement [flag.Value]
// much like [flag.Func]. The difference being that
func newFlagValue[T any](parseFn func(string) error) flag.Value {
	valueFn := genericFuncValue[T](parseFn)
	// `bool` flags don't require a value and this
	// must be conveyed to the [flag] package.
	if _, isBool := any([0]T{}).([0]bool); !isBool {
		return valueFn
	}
	return genericBoolFuncValue[T]{
		genericFuncValue: valueFn,
	}
}

func flagAlreadyProvidedErr(value any) error {
	// NOTE: [flag.Parse]'s error already
	// includes the flag name, etc.
	return fmt.Errorf("value already provided previously: %v", value)
}
