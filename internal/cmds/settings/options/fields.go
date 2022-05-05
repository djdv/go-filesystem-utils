package options

import (
	"context"
	"reflect"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

type fieldParam = generic.Couple[reflect.StructField, parameters.Parameter]

func accumulateOptions(ctx context.Context, fields runtime.SettingsFields,
	params parameters.Parameters, makers []OptionConstructor,
) ([]cmds.Option, error) {
	var (
		subCtx, cancel    = context.WithCancel(ctx)
		fieldParams, errs = skipEmbbedded(subCtx, fields, params)
		cmdsOptions       = make([]cmds.Option, 0, cap(params))
	)
	defer cancel()
	for pairOrErr := range generic.CtxEither(subCtx, fieldParams, errs) {
		if err := pairOrErr.Right; err != nil {
			return nil, err
		}
		var (
			fieldAndParam = pairOrErr.Left
			field         = fieldAndParam.Left
			param         = fieldAndParam.Right
			opt, err      = newSettingsOption(field, param, makers)
		)
		if err != nil {
			return nil, err
		}
		cmdsOptions = append(cmdsOptions, opt)
	}
	return cmdsOptions, nil
}

func skipEmbbedded(ctx context.Context, fields runtime.SettingsFields,
	params parameters.Parameters,
) (<-chan fieldParam, <-chan error) {
	var (
		fieldParams = make(chan fieldParam, cap(fields)+cap(params))
		errs        = make(chan error)
	)
	go func() {
		defer close(fieldParams)
		defer close(errs)
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
	return fieldParams, errs
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
