package commands

import (
	"flag"
	"strconv"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/commands/client"
	"github.com/djdv/go-filesystem-utils/internal/commands/daemon"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/multiformats/go-multiaddr"
)

type clientFlagCallbackFunc func(options []client.Option)

// inheritClientFlags wraps [bindClientFlags], providing a callback which
// upserts the result of `transformFn` into `superSet`.
// If `defaults` are provided, they're appended before any
// [flag.Value]'s `Set` method is called.
func inheritClientFlags[
	optionsPtr optionsReference[optionSlc, optionT, T],
	optionSlc optionSlice[optionT, T],
	optionT generic.OptionFunc[T],
	transformFunc func(...client.Option) optionT,
	T any,
](
	flagSet *flag.FlagSet,
	superSet optionsPtr,
	defaults []client.Option, transformFn transformFunc,
) {
	var (
		upsertFn = generic.UpsertSlice(superSet)
		wrapFn   = func(options []client.Option) {
			element := transformFn(options...)
			upsertFn(element)
		}
	)
	if len(defaults) > 0 {
		wrapFn(defaults)
	}
	bindClientFlags(flagSet, defaults, wrapFn)
}

// bindClientFlags will call `callbackFn` every time a flag
// value is set. Passing in the accumulated [Option] slice.
// `defaults` are optional (can be nil).
func bindClientFlags(flagSet *flag.FlagSet, defaults []client.Option, callbackFn clientFlagCallbackFunc) {
	accumulator := &defaults
	bindServerFlag(flagSet, accumulator, callbackFn)
	bindVerboseFlag(flagSet, accumulator, callbackFn)
}

func bindServerFlag(flagSet *flag.FlagSet, accumulator *[]client.Option, callbackFn clientFlagCallbackFunc) {
	const (
		name  = daemon.FlagServer
		usage = "file system service `maddr`"
	)
	assignFn := func(maddr multiaddr.Multiaddr) {
		*accumulator = append(*accumulator, client.WithAddress(maddr))
		callbackFn(*accumulator)
	}
	setValueOnce(
		flagSet, name, usage,
		multiaddr.NewMultiaddr, assignFn,
	)
	flagSet.Lookup(name).
		DefValue = daemon.DefaultAPIMaddr().String()
}

func bindVerboseFlag(flagSet *flag.FlagSet, accumulator *[]client.Option, callbackFn clientFlagCallbackFunc) {
	const (
		name  = "verbose"
		usage = "enable client message logging"
	)
	assignFn := func(verbose bool) {
		*accumulator = append(*accumulator, client.WithVerbosity(verbose))
		callbackFn(*accumulator)
	}
	setValueOnce(
		flagSet, name, usage,
		strconv.ParseBool, assignFn,
	)
}

func bindExitFlag(flagSet *flag.FlagSet, accumulator *[]client.Option, callbackFn clientFlagCallbackFunc) {
	const (
		name  = daemon.FlagExitAfter
		usage = "passed to the daemon command if we launch it" +
			"\n(refer to daemon's helptext)"
	)
	assignFn := func(interval time.Duration) {
		*accumulator = append(*accumulator, client.WithExitInterval(interval))
		callbackFn(*accumulator)
	}
	setValueOnce(
		flagSet, name, usage,
		time.ParseDuration, assignFn,
	)
	// NOTE: This CLI default value intentionally diverges
	// from whatever the daemon library uses as a default value.
	const defaultExitInterval = 30 * time.Second
	flagSet.Lookup(name).
		DefValue = defaultExitInterval.String()
}
