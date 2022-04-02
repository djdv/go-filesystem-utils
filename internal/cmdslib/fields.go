package cmdslib

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

var (
	errTooFewFields = errors.New("settings struct has more parameters than fields")
	//errGoldilocks = errors.New("just enough fields")
	errTooManyFields = errors.New("settings struct has more fields than parameters")
)

type (
	StructFields <-chan reflect.StructField

	ParamField struct {
		parameters.Parameter
		reflect.StructField
	}
	ParamFields = <-chan ParamField
)

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
		ForEachOrError(ctx, fields, nil, relayOrExpand)
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
		ProcessResults(ctx, fields, prefixed, nil, descend)
	}()
	return prefixed
}

func BindParameterFields[settings any,
	setPtr SettingsConstraint[settings]](ctx context.Context) (ParamFields, errCh, error) {
	typ, err := checkType[settings, setPtr]()
	if err != nil {
		return nil, nil, err
	}

	var (
		// MAGIC: We know our parameter methods never use data,
		// so we call them directly with a nil pointer.
		// This just avoids allocating an unnecessary struct value.
		// If needed, we could instantiate and call `settings.Parameters` instead.
		params      = setPtr.Parameters(nil, ctx)
		paramFields = make(chan ParamField, cap(params))
		errs        = make(chan error)
	)
	go func() {
		defer close(paramFields)
		defer close(errs)
		var (
			baseFields = generateFields(ctx, typ)
			allFields  = expandFields(ctx, baseFields)
			bindParams = func(field reflect.StructField) (ParamField, error) {
				select {
				case parameter, ok := <-params:
					if !ok {
						return ParamField{}, errTooManyFields
					}
					binding := ParamField{
						Parameter:   parameter,
						StructField: field,
					}
					return binding, nil

				case <-ctx.Done():
					return ParamField{}, ctx.Err()
				}
			}
		)
		ProcessResults(ctx, allFields, paramFields, errs, bindParams)
		if err := checkParameterCount(ctx, params); err != nil {
			select {
			case errs <- err:
			case <-ctx.Done():
			}
		}
	}()

	return paramFields, errs, nil
}

func checkParameterCount(ctx context.Context, params parameters.Parameters) error {
	var extraParams []string
out:
	for {
		select {
		case extra, ok := <-params:
			if !ok {
				break out
			}
			name := extra.Name(parameters.CommandLine)
			extraParams = append(extraParams, name)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if extraParams != nil {
		errStr := strings.Join(extraParams, ", ")
		return fmt.Errorf("%w: %s", errTooFewFields, errStr)
	}
	return nil
}
