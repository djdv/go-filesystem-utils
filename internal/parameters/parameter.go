// Package parameters provides abstractions around program parameters.
// Both creation and consumption, to and from sources such as the command-line,
// environment variables, HTTP requests, etc.
//
// Primarily by utilizing a struct convention, reflection, and the Go runtime to infer
// intent automatically.
package parameters

import (
	"errors"
	"strings"
)

type (
	// SourceID represents a value source/store.
	// Such as the command-line, process environment, etc.
	SourceID uint

	// Parameter contains methods
	// to describe its argument and various names for it.
	Parameter interface {
		// The primary names to be used with this parameter.
		Name(SourceID) string
		// Secondary names to be used with this parameter.
		// E.g. short names, deprecated names, alternate names, etc.
		Aliases(SourceID) []string
		// A string that describes what this parameter influences.
		Description() string
	}
	Parameters []Parameter

	// Settings are expected to be implemented by structs
	// that follow a specific convention.
	//
	// The library will look for the tag:
	//   `parameters:"settings"`
	// within the struct
	// (either directly or recursively via embedded structs)
	//
	// The first field declared in the struct that contains the tag,
	// will be associated with the first parameter returned from
	// the `Parameters` method of this interface, and so on.
	// (I.e. The order of fields in the struct, and the order of parameters returned,
	// must match.)
	Settings interface {
		Parameters() Parameters
	}
)

const (
	_ SourceID = iota
	CommandLine
	Environment
)

var (
	ErrUnexpectedSourceID = errors.New("unexpected source ID")
)

func (params Parameters) String() string {
	parameterNames := make([]string, len(params))
	for i, param := range params {
		parameterNames[i] = param.Name(CommandLine)
	}
	return strings.Join(parameterNames, ", ")
}
