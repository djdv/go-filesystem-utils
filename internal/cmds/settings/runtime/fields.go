package runtime

import (
	"context"
	"errors"
	"reflect"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

var (
	errTooFewFields  = errors.New("more parameters than fields")
	errTooManyFields = errors.New("more fields than parameters")
)

type (
	StructFields <-chan reflect.StructField

	ParamField struct {
		parameters.Parameter
		reflect.StructField
	}
	ParamFields = <-chan ParamField
)

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

func expandFields(ctx context.Context, fields StructFields) StructFields {
	out := make(chan reflect.StructField, cap(fields))
	go func() {
		subCtx, cancel := context.WithCancel(ctx)
		defer close(out)
		defer cancel()
		relayOrExpand := func(field reflect.StructField) error {
			if !field.Anonymous ||
				field.Type.Kind() != reflect.Struct {
				select {
				case out <- field:
				case <-ctx.Done():
					return ctx.Err()
				}
				return nil
			}
			var (
				embeddedFields = generateFields(subCtx, field.Type)
				prefixedFields = prefixIndex(subCtx, field.Index, embeddedFields)
				recursedFields = expandFields(subCtx, prefixedFields)
			)
			for field := range recursedFields {
				select {
				case out <- field:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}
		generic.ForEachOrError(ctx, fields, nil, relayOrExpand)
	}()
	return out
}

func prefixIndex(ctx context.Context, prefix []int, fields StructFields) StructFields {
	prefixed := make(chan reflect.StructField, cap(fields))
	go func() {
		defer close(prefixed)
		descend := func(field reflect.StructField) (reflect.StructField, error) {
			field.Index = append(prefix, field.Index...)
			return field, nil
		}
		generic.ProcessResults(ctx, fields, prefixed, nil, descend)
	}()
	return prefixed
}
