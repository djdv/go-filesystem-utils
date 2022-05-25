package runtime_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

func TestRuntime(t *testing.T) {
	t.Parallel()
	t.Run("fields", testFields)
}

type (
	fieldSettings struct {
		A, B, C, D, E, F, G, H, I, J, K, L, M, N, O, P, Q, R, S, T, U, V, W, X, Y, Z struct{}
	}
	fieldParam struct{ reflect.StructField }
)

func (sf fieldParam) Name(parameters.Provider) string      { return sf.StructField.Name }
func (sf fieldParam) Description() string                  { return sf.Type.String() }
func (sf fieldParam) Aliases(parameters.Provider) []string { return nil }

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
	t.Run("canceled", canceledFields)
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

func canceledFields(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fields, err := runtime.ReflectFields[*fieldSettings](ctx)
	if err != nil {
		t.Fatal(err)
	}
	for field := range fields {
		t.Errorf("received field (%v) after canceled", field)
	}
}
