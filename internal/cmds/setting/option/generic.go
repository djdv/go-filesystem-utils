package option

import (
	"fmt"
	"reflect"

	cmds "github.com/ipfs/go-ipfs-cmds"
)

type (
	// Constructor is the binding of a value type
	// with its corresponding `Option` constructor.
	Constructor interface {
		Type() reflect.Type
		NewOption(name, description string, aliases ...string) cmds.Option
	}
	// ParseFunc interprets the string as/into a Go value.
	ParseFunc[valueType any]          func(string) (valueType, error)
	genericConstructor[valueType any] struct {
		parser ParseFunc[valueType]
	}

	genericOption[defaultType any] struct {
		defaultValue defaultType
		parser       ParseFunc[defaultType]
		description  string
		names        []string
	}
)

func (gc genericConstructor[valueType]) Type() reflect.Type {
	return reflect.TypeOf((*valueType)(nil)).Elem()
}

func (gc genericConstructor[valueType]) NewOption(name, desc string, aliases ...string) cmds.Option {
	return newOption(name, desc, gc.parser, aliases...)
}

func (op *genericOption[_]) Name() string                  { return op.names[0] }
func (op *genericOption[_]) Names() []string               { return op.names }
func (op *genericOption[_]) Description() string           { return op.description }
func (op *genericOption[_]) Default() any                  { return op.defaultValue }
func (op *genericOption[_]) Parse(str string) (any, error) { return op.parser(str) }

func (op *genericOption[defaultType]) Type() reflect.Kind {
	return reflect.TypeOf((*defaultType)(nil)).Elem().Kind()
}

func (op *genericOption[defaultType]) WithDefault(value any) cmds.Option {
	typed, ok := value.(defaultType)
	if !ok {
		err := fmt.Errorf("invalid type for option's value"+
			"\n\tgot: %T"+
			"\n\twant: %T",
			value, typed,
		)
		panic(err)
	}
	op.defaultValue = typed
	return op
}

// NewOptionConstructor wraps a [ParseFunc],
// creating a [cmds.Option] constructor for a particular type.
func NewOptionConstructor[valueType any](parser ParseFunc[valueType]) Constructor {
	return genericConstructor[valueType]{parser: parser}
}

func newOption[valueType any](name, description string,
	parser ParseFunc[valueType], aliases ...string,
) cmds.Option {
	return &genericOption[valueType]{
		names:       append([]string{name}, aliases...),
		description: description,
		parser:      parser,
	}
}
