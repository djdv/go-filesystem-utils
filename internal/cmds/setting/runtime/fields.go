package runtime

import (
	"context"
	"reflect"
)

// SettingsFields is a series of struct fields
// derived from the `Settings` underlying type.
type SettingsFields = <-chan reflect.StructField

// ReflectFields accepts a `[*struct]` type-parameter,
// that also implements the `Settings` interface.
//
// The struct's top-level fields are sent to the output channel.
func ReflectFields[setPtr SettingsType[set], set any](ctx context.Context,
) (SettingsFields, error) {
	typ, err := checkType[setPtr]()
	if err != nil {
		return nil, err
	}
	return MustGenerateFields(ctx, typ), nil
}

// MustGenerateFields accepts a struct-type
// and returns its fields as a channel.
func MustGenerateFields(ctx context.Context, typ reflect.Type) SettingsFields {
	var (
		fieldCount = typ.NumField()
		fields     = make(chan reflect.StructField, fieldCount)
	)
	go func() {
		defer close(fields)
		for i := 0; i < fieldCount; i++ {
			if ctx.Err() != nil {
				return
			}
			fields <- typ.Field(i)
		}
	}()
	return fields
}
