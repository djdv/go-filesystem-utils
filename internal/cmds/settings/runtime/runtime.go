// Package runtime interfaces with Go's runtime and the `parameter.Settings` interface.
package runtime

import (
	"context"
	"fmt"
	"reflect"

	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type (
	// SettingsType should be of type pointer to struct,
	// which implements the Settings interface.
	SettingsType[Settings any] interface {
		*Settings
		parameters.Settings
	}
	// SettingsFields is a series of struct fields
	// derived from the `Settings` underlying type.
	SettingsFields = <-chan reflect.StructField

	constErr string
)

func (errStr constErr) Error() string { return string(errStr) }

const (
	// ErrUnassignable may be returned when assignment to a value references
	// is not allowed by Go's runtime rules.
	ErrUnassignable constErr = "cannot assign"

	// ErrUnexpectedType may be returned when a type parameter
	// does not match an expected underlying type (of a `Settings` implementation).
	ErrUnexpectedType constErr = "unexpected type"
)

// ReflectFields accepts a `[*struct]` type-parameter,
// that also implements the `Settings` interface.
//
// The struct's top-level fields are sent to the output channel.
func ReflectFields[setPtr SettingsType[settings], settings any](ctx context.Context,
) (SettingsFields, error) {
	typ, err := checkType[setPtr]()
	if err != nil {
		return nil, err
	}
	return generateFields(ctx, typ), nil
}

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

func generateFields(ctx context.Context, setTyp reflect.Type) SettingsFields {
	var (
		fieldCount = setTyp.NumField()
		fields     = make(chan reflect.StructField, fieldCount)
	)
	go func() {
		defer close(fields)
		for i := 0; i < fieldCount; i++ {
			if ctx.Err() != nil {
				return
			}
			fields <- setTyp.Field(i)
		}
	}()
	return fields
}
