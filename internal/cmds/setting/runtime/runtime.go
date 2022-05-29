// Package runtime interfaces with Go's runtime and the `parameter.Settings` interface.
package runtime

import (
	"fmt"
	"reflect"

	"github.com/djdv/go-filesystem-utils/internal/parameter"
)

type (
	// SettingsType should be a pointer to a struct
	// which implements the Settings interface.
	SettingsType[Settings any] interface {
		*Settings
		parameter.Settings
	}

	constError string
)

func (errStr constError) Error() string { return string(errStr) }

const (
	// ErrUnassignable may be returned when assignment to a value references
	// is not allowed by Go's runtime rules.
	ErrUnassignable constError = "cannot assign"

	// ErrUnexpectedType may be returned when a type parameter
	// does not match an expected underlying type (of a `Settings` implementation).
	ErrUnexpectedType constError = "unexpected type"
)

func checkType[setPtr SettingsType[settings], settings any]() (reflect.Type, error) {
	var (
		setType  = reflect.TypeOf((setPtr)(nil))
		typ      = setType.Elem()
		kind     = typ.Kind()
		isStruct = kind == reflect.Struct
	)
	if !isStruct {
		err := fmt.Errorf("%w:"+
			" got: `%s`"+
			" want: `struct`",
			ErrUnexpectedType,
			kind,
		)
		return nil, err
	}
	return typ, nil
}
