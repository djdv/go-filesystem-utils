package runtime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

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

// TODO: rename?
func BindParameterFields(ctx context.Context, fields StructFields, params parameters.Parameters) (ParamFields, <-chan error) {
	var (
		paramFields = make(chan ParamField, cap(params))
		errs        = make(chan error)
	)
	go func() {
		defer close(paramFields)
		defer close(errs)
		bindParams := func(field reflect.StructField) (ParamField, error) {
			select {
			case parameter, ok := <-params:
				if !ok {
					// TODO: annotate error with counts? or something.
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
		generic.ProcessResults(ctx, fields, paramFields, errs, bindParams)
		if err := checkParameterCount(ctx, params); err != nil {
			select {
			case errs <- err:
			case <-ctx.Done():
			}
		}
	}()

	return paramFields, errs
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
		return fmt.Errorf("%w - extra parameters: %s", errTooFewFields, errStr)
	}
	return nil
}

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
