package runtime

import (
	"context"
	"reflect"
)

type StructFields = <-chan reflect.StructField

func ReflectFields[setPtr SettingsConstraint[set], set any](ctx context.Context,
) (StructFields, error) {
	typ, err := checkType[set]()
	if err != nil {
		return nil, err
	}
	return generateFields(ctx, typ), nil
}

func generateFields(ctx context.Context, setTyp reflect.Type) StructFields {
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
