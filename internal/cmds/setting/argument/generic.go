package argument

import "reflect"

type (
	Parser interface {
		Type() reflect.Type
		Parse(string) (any, error)
	}

	ParseFunc[valueType any]     func(string) (valueType, error)
	genericParser[valueType any] struct {
		parser func(string) (valueType, error)
	}
)

func (gp genericParser[valueType]) Type() reflect.Type {
	return reflect.TypeOf((*valueType)(nil)).Elem()
}

func (gp genericParser[valueType]) Parse(s string) (any, error) { return gp.parser(s) }

// NewOptionConstructor wraps a [ParseFunc],
// creating a [cmds.Option] constructor for a particular type.
func NewParser[valueType any](parser ParseFunc[valueType]) Parser {
	return genericParser[valueType]{parser: parser}
}
