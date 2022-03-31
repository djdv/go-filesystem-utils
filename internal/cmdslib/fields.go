package cmdslib

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

var errTooFewFields = errors.New("not enough fields")

type (
	structFields <-chan reflect.StructField

	ParamField struct {
		parameters.Parameter
		reflect.StructField
	}
	ParamFields = <-chan ParamField
)

func generateFields(ctx context.Context, setTyp reflect.Type) structFields {
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

func expandFields(ctx context.Context, fields structFields) structFields {
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

func prefixIndex(ctx context.Context, prefix []int, fields structFields) structFields {
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

func BindParameterFields(ctx context.Context, set parameters.Settings) (ParamFields, errCh) {
	var (
		parameters  = set.Parameters()
		paramCount  = len(parameters)
		paramFields = make(chan ParamField, paramCount)
		errs        = make(chan error)
	)
	go func() {
		defer close(paramFields)
		defer close(errs)

		typ, err := checkType(set)
		if err != nil {
			select {
			case errs <- err:
			case <-ctx.Done():
			}
			return
		}
		var (
			baseFields = generateFields(ctx, typ)
			allFields  = expandFields(ctx, baseFields)

			paramIndex int
			bindParams = func(field reflect.StructField) (ParamField, error) {
				if paramIndex >= paramCount {
					// TODO: export error
					return ParamField{}, errors.New("settings struct has too many fields")
				}
				var (
					parameter = parameters[paramIndex]
					binding   = ParamField{
						Parameter:   parameter,
						StructField: field,
					}
				)
				paramIndex++
				return binding, nil
			}
		)
		ProcessResults(ctx, allFields, paramFields, errs, bindParams)
		if ctx.Err() != nil {
			return // Don't validate if we're canceled.
		}
		if err := checkParameterCount(paramIndex, paramCount, typ, parameters); err != nil {
			select {
			case errs <- err:
			case <-ctx.Done():
			}
		}
	}()

	return paramFields, errs
}

func checkParameterCount(count, expected int, typ reflect.Type,
	parameters parameters.Parameters,
) (err error) {
	if count != expected {
		remainder := parameters[count:]
		err = fmt.Errorf("%w:"+
			"\n\tgot: %d for %s"+
			"\n\twant: %d to fit remaining parameters [%s]",
			errTooFewFields,
			count, typ.Name(),
			expected, remainder,
		)
	}
	return
}
