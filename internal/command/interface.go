package command

import (
	"context"
	"flag"

	"github.com/djdv/go-filesystem-utils/internal/generic"
)

type (
	Command interface {
		// Name returns a human friendly name of the command.
		// Which may be used to identify commands,
		// as well as decorate user facing help-text.
		Name() string

		// Synopsis returns a single-line string describing the command.
		Synopsis() string

		// Usage returns an arbitrarily long string explaining how to use the command.
		Usage() string

		// Subcommands returns a list of subcommands (if any).
		Subcommands() []Command

		// Execute executes the command, with or without any arguments.
		Execute(ctx context.Context, args ...string) error
	}

	// A FlagBinder should call the relevant `Var` methods of the [flag.FlagSet],
	// with each of it's flag variable references.
	// E.g. a struct would pass pointers to each of its fields,
	// to `FlagSet.Var(&structABC.fieldXYZ, ...)`.
	FlagBinder interface {
		BindFlags(*flag.FlagSet)
	}

	// FlagSettings is a constraint that permits any reference type
	// that can bind its value setter(s), to a [flag.FlagSet].
	FlagSettings[settings any] interface {
		*settings
		FlagBinder
	}
)

// ErrUsage may be returned from Execute if the provided arguments
// do not match the expectations of the given command.
// E.g. arguments in the wrong format/type, too few/many arguments, etc.
const ErrUsage = generic.ConstError("command called with unexpected arguments")
