package reflect

import (
	"reflect"

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
