// Package parameter provides an abstraction around program "formal parameters"
// and exposes a set of argument providers.
package parameter

import "context"

type (
	// A Provider identifies a parameter source,
	// such as the command-line, process environment, etc.
	Provider uint

	// A Parameter provides methods to describe itself
	// and serves as the "formal parameter"
	// in relation to an argument (the "actual parameter").
	//
	// I.e. it is the left side within the command-line argument: `--parameter-name="argument value"`.
	Parameter interface {
		// Name returns the primary name used within the Provider's system.
		//
		// I.e. the parameter's canonical name.
		Name(Provider) string
		// Aliases returns additional names that may also be valid within the Provider's system.
		//
		// E.g. short names, deprecated names, alternate names, etc.
		Aliases(Provider) []string
		// Description returns a string that should describe what
		// this parameter influences within the program it's used in.
		Description() string
	}
	// Parameters is a shorthand alias for a sequence of `Parameter`s.
	Parameters = <-chan Parameter

	// Settings is the interface that wraps a Parameters method.
	//
	// Implementations of Settings are typically value containers (such as a struct)
	// and expose a sequence of Parameters which contain metadata for respective values.
	//
	// E.g a struct with field `Port`, may have a corresponding `Parameter` whose `Name` method returns "server-port".
	Settings interface {
		Parameters(context.Context) Parameters
	}
)

//go:generate stringer -type=Provider -linecomment
const (
	_           Provider = iota
	CommandLine          // command-line
	Environment          // PROCESS_ENVIRONMENT
)
