package option

import (
	"context"
	"fmt"
	"reflect"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameter"
)

type (
	structField     = reflect.StructField
	structFields    = runtime.SettingsFields
	fieldParameter  = parameter.Parameter
	fieldParameters = parameter.Parameters
	fieldAndParam   = generic.Couple[structField, fieldParameter]
	fieldAndParams  = <-chan fieldAndParam
	errors          = <-chan error
)

func fieldsFromSettings[setPtr runtime.SettingsType[settings], settings any](ctx context.Context,
) (structFields, errors, error) {
	fields, err := runtime.ReflectFields[setPtr](ctx)
	if err != nil {
		return nil, nil, err
	}
	validFields, errs := checkFields(ctx, fields)
	return validFields, errs, nil
}

func fieldParamsFromSettings[setPtr runtime.SettingsType[settings],
	settings any](ctx context.Context,
) (fieldAndParams, errors, error) {
	validFields, errs, err := fieldsFromSettings[setPtr](ctx)
	if err != nil {
		return nil, nil, err
	}

	var (
		params      = setPtr.Parameters(nil, ctx)
		fieldParams = skipEmbbedded(ctx, validFields, params)
	)
	return fieldParams, errs, nil
}

func checkFields(ctx context.Context, fields structFields) (structFields, errors) {
	var (
		relay = make(chan structField, cap(fields))
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

func skipEmbbedded(ctx context.Context, fields structFields, params fieldParameters) fieldAndParams {
	fieldParams := make(chan fieldAndParam, cap(fields)+cap(params))
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
				case fieldParams <- fieldAndParam{Left: field, Right: param}:
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

func isEmbeddedField(field structField) bool {
	return field.Anonymous && field.Type.Kind() == reflect.Struct
}

func skipStruct(ctx context.Context, field structField, params fieldParameters) {
	for skipCount := field.Type.NumField(); skipCount != 0; skipCount-- {
		select {
		case <-params:
		case <-ctx.Done():
			return
		}
	}
}
