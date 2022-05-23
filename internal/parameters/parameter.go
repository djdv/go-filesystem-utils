// Package parameters provides an abstraction around program "formal parameters"
// and exposes a set of argument sources.
package parameters

import "context"

type (
	// A SourceID represents an argument value source,
	// such as the command-line, process environment, etc.
	SourceID uint

	// A Parameter provides methods to describe itself,
	// and serves as the "formal parameter" in relation to an argument ("actual parameter").
	//
	// I.e. it is the left side within the command-line argument: `--parameter-name="argument value"`.
	Parameter interface {
		// Name returns the primary name used within the SourceID's system.
		//
		// I.e. the parameter's canonical name.
		Name(SourceID) string
		// Aliases returns additional names that may also be valid within the SourceID's system.
		//
		// E.g. short names, deprecated names, alternate names, etc.
		Aliases(SourceID) []string
		// Description returns a string that should describe what
		// this parameter influences within the program it's used in.
		Description() string
	}
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

//go:generate stringer -type=SourceID -linecomment
const (
	_           SourceID = iota
	CommandLine          // command-line
	Environment          // PROCESS_ENVIRONMENT
)
