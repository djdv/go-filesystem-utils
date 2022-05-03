package environment_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/environment"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

func TestEnvironment(t *testing.T) {
	t.Run("valid", testEnvironmentValid)
	t.Run("invalid", testEnvironmentInvalid)
}

type (
	envSettings struct {
		TestField  bool
		TestField2 int
		UnsetField int
	}
)

func (*envSettings) Parameters(ctx context.Context) parameters.Parameters {
	return emptyParams[*envSettings](ctx)
}

func testEnvironmentValid(t *testing.T) {
	var (
		ctx          = context.Background()
		wantSettings = &envSettings{
			TestField:  true,
			TestField2: 2,
		}
	)
	settingsToEnv(t, wantSettings)
	var (
		sources       = []runtime.SetFunc{environment.ValueSource()}
		settings, err = runtime.Parse[*envSettings](ctx, sources)
	)
	if err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual(wantSettings, settings) {
		t.Errorf("settings field values do not match input values"+
			"\n\tgot: %#v"+
			"\n\twant: %#v",
			settings, wantSettings)
	}
}

func testEnvironmentInvalid(t *testing.T) {
	t.Run("values", badValues)
	t.Run("canceled", cancelParse)
}

func badValues(t *testing.T) {
	ctx := context.Background()
	for param := range (*envSettings).Parameters(nil, ctx) {
		t.Setenv(
			param.Name(parameters.Environment),
			"invalid non-string value",
		)
	}
	var (
		sources       = []runtime.SetFunc{environment.ValueSource()}
		settings, err = runtime.Parse[*envSettings](ctx, sources)
	)
	if err == nil {
		t.Error("expected Parse to return an error, but it did not")
	}
	if settings != nil {
		t.Error("expected Parse to return nil settings, but it did not")
	}
}

func cancelParse(t *testing.T) {
	t.Run("cancel context", func(t *testing.T) {
		var (
			expectedErr             = context.Canceled
			testContext, testCancel = context.WithCancel(context.Background())
		)
		testCancel()
		var (
			sources = []runtime.SetFunc{environment.ValueSource()}
			_, err  = runtime.Parse[*envSettings](testContext, sources)
		)
		if !errors.Is(err, expectedErr) {
			t.Errorf("error value does not match"+
				"\n\tgot: %v"+
				"\n\twant: %v",
				err, expectedErr,
			)
		}
	})
}

func emptyParams[setPtr runtime.SettingsConstraint[set], set any](ctx context.Context) parameters.Parameters {
	var (
		fieldCount  = reflect.TypeOf((*set)(nil)).Elem().NumField()
		emptyParams = make([]runtime.CmdsParameter, fieldCount)
	)
	return runtime.MustMakeParameters[setPtr](ctx, emptyParams)
}

func settingsToEnv(t *testing.T, set parameters.Settings) {
	t.Helper()
	var (
		ctx           = context.Background()
		params        = set.Parameters(ctx)
		settingsValue = reflect.ValueOf(set).Elem()
		fields        = reflect.VisibleFields(settingsValue.Type())
	)

	var fieldIndex int
	for param := range params {
		var (
			field        = fields[fieldIndex]
			fieldsIndex  = field.Index // Field's Index - possessive 's'
			reflectValue = settingsValue.FieldByIndex(fieldsIndex)
		)
		fieldIndex++
		if reflectValue.IsZero() {
			continue
		}
		var (
			key   = param.Name(parameters.Environment)
			value = reflectValue.Interface()
		)
		t.Setenv(key, fmt.Sprintf("%v", value))
	}
}
