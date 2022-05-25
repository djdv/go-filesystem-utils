package options

import (
	"context"
	"fmt"
	"reflect"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type fieldParam = generic.Couple[reflect.StructField, parameters.Parameter]

func checkFields(ctx context.Context,
	fields runtime.SettingsFields,
) (runtime.SettingsFields, <-chan error) {
	var (
		relay = make(chan reflect.StructField, cap(fields))
		errs  = make(chan error)
	)
	go func() {
		defer close(relay)
		defer close(errs)
		for field := range fields {
			if !field.IsExported() {
				err := fmt.Errorf("%w:"+
					" refusing to create option for unassignable field"+
					" - `%s` is not exported",
					runtime.ErrUnassignable,
					field.Name,
				)
				select {
				case errs <- err:
				case <-ctx.Done():
				}
				return
			}
			select {
			case relay <- field:
			case <-ctx.Done():
				return
			}
		}
	}()
	return relay, errs
}

func skipEmbbedded(ctx context.Context, fields runtime.SettingsFields,
	params parameters.Parameters,
) <-chan fieldParam {
	fieldParams := make(chan fieldParam, cap(fields)+cap(params))
	go func() {
		defer close(fieldParams)
		for field := range fields {
			if isEmbeddedField(field) {
				skipStruct(ctx, field, params)
				continue
			}
			select {
			case param, ok := <-params:
				if !ok {
					return
				}
				select {
				case fieldParams <- fieldParam{Left: field, Right: param}:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return fieldParams
}

func isEmbeddedField(field reflect.StructField) bool {
	return field.Anonymous && field.Type.Kind() == reflect.Struct
}

func skipStruct(ctx context.Context, field reflect.StructField, params parameters.Parameters) {
	for skipCount := field.Type.NumField(); skipCount != 0; skipCount-- {
		select {
		case <-params:
		case <-ctx.Done():
			return
		}
	}
}
