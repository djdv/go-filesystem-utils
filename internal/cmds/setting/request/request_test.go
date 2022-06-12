package request_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	settings "github.com/djdv/go-filesystem-utils/internal/cmds/setting"
	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/argument"
	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/request"
	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/runtime"
	"github.com/djdv/go-filesystem-utils/internal/parameter"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

type (
	argSettings struct {
		TestField  bool
		TestField2 int
		UnsetField int
	}

	argSettingsSlice struct {
		TestField []time.Duration
	}
)

func emptyParams[setPtr runtime.SettingsType[set], set any](ctx context.Context) parameter.Parameters {
	var (
		fieldCount  = reflect.TypeOf((*set)(nil)).Elem().NumField()
		emptyParams = make([]settings.CmdsParameter, fieldCount)
	)
	return settings.MustMakeParameters[setPtr](ctx, emptyParams)
}

func (*argSettings) Parameters(ctx context.Context) parameter.Parameters {
	return emptyParams[*argSettings](ctx)
}

func (*argSettingsSlice) Parameters(ctx context.Context) parameter.Parameters {
	return emptyParams[*argSettingsSlice](ctx)
}

func TestArguments(t *testing.T) {
	t.Parallel()
	t.Run("valid", argumentsValid)
	t.Run("invalid", argumentsInvalid)
}

func argumentsValid(t *testing.T) {
	t.Parallel()
	t.Run("noop", noopParse)
	t.Run("provided", argsParse)
}

func noopParse(t *testing.T) {
	t.Parallel()
	// This test will only show up in tracing, like the test coverage report.
	// It's purpose is to make sure `Parse` breaks out early when it can/should.
	// I.e. When the request has no user-defined settings (cmdslib native options don't count)
	var (
		ctx      = context.Background()
		req, err = cmds.NewRequest(ctx, nil,
			cmds.OptMap{cmds.EncLong: "text"},
			nil, nil, &cmds.Command{})
		sources = []argument.SetFunc{request.ValueSource(req)}
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := argument.Parse[*argSettings](ctx, sources); err != nil {
		t.Fatal(err)
	}
}

func argsParse(t *testing.T) {
	t.Parallel()
	t.Run("single", argsParseSingle)
	t.Run("slice", argsParseSlice)
}

func argsParseSingle(t *testing.T) {
	t.Parallel()
	type testType = argSettings
	var (
		ctx          = context.Background()
		wantSettings = &testType{
			TestField:  true,
			TestField2: 2,
		}
		cmdOpts  = settingsToValueOpts(wantSettings)
		req, err = cmds.NewRequest(ctx, nil, cmdOpts,
			nil, nil, &cmds.Command{})
		sources = []argument.SetFunc{request.ValueSource(req)}
	)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := argument.Parse[*testType](ctx, sources)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(wantSettings, settings) {
		t.Errorf("settings field values do not match input values"+
			"\n\tgot: %#v"+
			"\n\twant: %#v",
			settings, wantSettings)
	}
}

func argsParseSlice(t *testing.T) {
	t.Parallel()
	type testType = argSettingsSlice
	var (
		ctx          = context.Background()
		wantSettings = &testType{
			TestField: []time.Duration{
				time.Second,
				2 * time.Second,
			},
		}
		cmdOpts  = settingsToStringOpts(wantSettings)
		req, err = cmds.NewRequest(ctx, nil, cmdOpts,
			nil, nil, &cmds.Command{})
		sources = []argument.SetFunc{request.ValueSource(req)}
		parsers = []argument.Parser{
			argument.NewParser(time.ParseDuration),
		}
	)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := argument.Parse[*testType](ctx, sources, parsers...)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(wantSettings, settings) {
		t.Errorf("settings field values do not match input values"+
			"\n\tgot: %#v"+
			"\n\twant: %#v",
			settings, wantSettings)
	}
}

func argumentsInvalid(t *testing.T) {
	t.Parallel()
	t.Run("canceled", cancelParse)
}

func cancelParse(t *testing.T) {
	t.Run("cancel context", func(t *testing.T) {
		var (
			expectedErr             = context.Canceled
			testContext, testCancel = context.WithCancel(context.Background())
			req, rErr               = cmds.NewRequest(testContext, nil, nil,
				nil, nil, &cmds.Command{})
		)
		if rErr != nil {
			t.Error(rErr)
		}
		testCancel()
		var (
			sources = []argument.SetFunc{request.ValueSource(req)}
			_, err  = argument.Parse[*argSettings](testContext, sources)
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

func settingsToOptsFn(set parameter.Settings, fn func(a any) any) cmds.OptMap {
	var (
		ctx           = context.Background()
		params        = set.Parameters(ctx)
		settingsValue = reflect.ValueOf(set).Elem()
		fields        = reflect.VisibleFields(settingsValue.Type())
		cmdOpts       = make(cmds.OptMap, cap(params))
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
			key   = param.Name(parameter.CommandLine)
			value = reflectValue.Interface()
		)
		cmdOpts[key] = fn(value)
	}

	return cmdOpts
}

func settingsToValueOpts(set parameter.Settings) cmds.OptMap {
	return settingsToOptsFn(set,
		func(a any) any { return a },
	)
}

func settingsToStringOpts(set parameter.Settings) cmds.OptMap {
	return settingsToOptsFn(set,
		func(a any) any {
			if reflect.TypeOf(a).Kind() == reflect.Slice {
				var (
					aValue  = reflect.ValueOf(a)
					aEnd    = aValue.Len()
					strings = make([]string, aEnd)
				)
				for i := 0; i != aEnd; i++ {
					strings[i] = fmt.Sprint(aValue.Index(i).Interface())
				}
				return strings
			}
			return fmt.Sprint(a)
		},
	)
}
