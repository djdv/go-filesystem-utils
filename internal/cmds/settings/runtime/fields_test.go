package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type (
	fieldSettings struct {
		A, B, C, D, E, F, G, H, I, J, K, L, M, N, O, P, Q, R, S, T, U, V, W, X, Y, Z struct{}
	}
	fieldParam struct{ reflect.StructField }
)

func (sf fieldParam) Name(parameters.SourceID) string      { return sf.StructField.Name }
func (sf fieldParam) Description() string                  { return sf.Type.String() }
func (sf fieldParam) Aliases(parameters.SourceID) []string { return nil }

func (fs *fieldSettings) Parameters(ctx context.Context) parameters.Parameters {
	var (
		fields = reflect.VisibleFields(reflect.TypeOf(fs).Elem())
		params = make(chan parameters.Parameter, len(fields))
	)
	go func() {
		defer close(params)
		for _, field := range fields {
			if ctx.Err() != nil {
				return
			}
			params <- fieldParam{field}
		}
	}()
	return params
}

func testFields(t *testing.T) {
	t.Parallel()
	t.Run("valid", fieldsValid)
	t.Run("invalid", fieldsInvalid)
}

func fieldsValid(t *testing.T) {
	t.Parallel()
	t.Run("struct", fieldsStruct)
}

func fieldsStruct(t *testing.T) {
	t.Parallel()
	t.Run("reflect", fieldsReflectStruct)
	t.Run("bind", fieldsBindStruct)
}

func fieldsReflectStruct(t *testing.T) {
	t.Parallel()
	var (
		ctx         = context.Background()
		fieldCount  = reflect.TypeOf((*fieldSettings)(nil)).Elem().NumField()
		fieldMap    = make(map[string]struct{}, fieldCount)
		fields, err = runtime.ReflectFields[*fieldSettings](ctx)
	)
	if err != nil {
		t.Fatal(err)
	}
	for field := range fields {
		name := field.Name
		if _, ok := fieldMap[name]; ok {
			t.Error("duplicate field returned:", name)
		}
		fieldMap[name] = struct{}{}
	}
	if gotFields := len(fieldMap); gotFields != fieldCount {
		t.Errorf("fields received does not match field count"+
			"\n\tgot: %#v"+
			"\n\twant: %#v",
			gotFields, fieldCount)
	}
}

func fieldsBindStruct(t *testing.T) {
	t.Parallel()
	var (
		ctx         = context.Background()
		pairs, errs = bindFields[*fieldSettings](t, ctx)
		check       = func(pair runtime.ParamField) error {
			var (
				paramElement  = pair.Description()
				structElement = pair.Type.String()
			)
			if paramElement != structElement {
				return fmt.Errorf("bound elements are not ordered correctly / data mismatch"+
					"\n\tparam element: %s"+
					"\n\tstruct element: %s",
					paramElement, structElement)
			}
			return nil
		}
	)
	if err := generic.ForEachOrError(ctx, pairs, errs, check); err != nil {
		t.Error(err)
	}
}

type (
	fieldsInvalidSettingsBool        bool
	fieldsInvalidSettingsFewerParams struct {
		A, B struct{}
	}
	fieldsInvalidSettingsFewerFields struct {
		A struct{}
	}
)

func (fieldsInvalidSettingsBool) Parameters(context.Context) parameters.Parameters { return nil }

func (fs *fieldsInvalidSettingsFewerParams) Parameters(ctx context.Context) parameters.Parameters {
	var (
		field  = reflect.TypeOf(fs).Elem().Field(0)
		params = make(chan parameters.Parameter, 1)
	)
	defer close(params)
	params <- fieldParam{field}
	return params
}

func (fs *fieldsInvalidSettingsFewerFields) Parameters(ctx context.Context) parameters.Parameters {
	var (
		field  = reflect.TypeOf(fs).Elem().Field(0)
		params = make(chan parameters.Parameter, 2)
	)
	defer close(params)
	params <- fieldParam{field}
	params <- fieldParam{field}
	return params
}

func fieldsInvalid(t *testing.T) {
	t.Parallel()
	t.Run("non-struct", fieldsInvalidType)
	t.Run("extra fields", fieldsInvalidFields)
	t.Run("extra params", fieldsInvalidParams)
}

func fieldsInvalidType(t *testing.T) {
	t.Parallel()
	var (
		ctx         = context.Background()
		expectedErr = runtime.ErrUnexpectedType
		_, err      = runtime.ReflectFields[*fieldsInvalidSettingsBool](ctx)
	)
	if !errors.Is(err, expectedErr) {
		t.Errorf("did not received expected error for invalid type"+
			"\n\tgot: %v"+
			"\n\twant: %v",
			err, expectedErr)
	}
}

func fieldsInvalidFields(t *testing.T) {
	t.Parallel()
	var (
		ctx         = context.Background()
		pairs, errs = bindFields[*fieldsInvalidSettingsFewerParams](t, ctx)
		check       = func(pair runtime.ParamField) error { return nil }
	)
	if err := generic.ForEachOrError(ctx, pairs, errs, check); err == nil {
		t.Errorf("did not received error for invalid struct (too many fields)")
	}
}

func fieldsInvalidParams(t *testing.T) {
	t.Parallel()
	var (
		ctx         = context.Background()
		pairs, errs = bindFields[*fieldsInvalidSettingsFewerFields](t, ctx)
		check       = func(pair runtime.ParamField) error { return nil }
	)
	if err := generic.ForEachOrError(ctx, pairs, errs, check); err == nil {
		t.Errorf("did not received error for invalid method (too many parameters)")
	}
}

func bindFields[setPtr runtime.SettingsConstraint[set], set any](
	t *testing.T,
	ctx context.Context,
) (runtime.ParamFields, <-chan error) {
	t.Helper()
	var (
		params      = (setPtr).Parameters(nil, ctx)
		fields, err = runtime.ReflectFields[setPtr](ctx)
		pairs, errs = runtime.BindParameterFields(ctx, fields, params)
	)
	if err != nil {
		t.Fatal(err)
	}
	return pairs, errs
}
