package parameters_test

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

func testEnvironment(t *testing.T) {
	var (
		ctx      = context.Background()
		settings = new(testPkgSettings)
		params   = settings.Parameters()
		clearEnv = func() {
			for _, param := range params {
				key := param.Name(parameters.Environment)
				if osErr := os.Unsetenv(key); osErr != nil {
					t.Errorf("failed to unset environment %q: %s",
						key, osErr)
				}
			}
		}

		wantSettings = new(testPkgSettings)
	)
	nonzeroValueSetter(wantSettings)

	// Make sure env is clear before and after the test.
	clearEnv()
	defer clearEnv()

	// Populate the env with our expected data.
	testSettingsToEnv(t, wantSettings)

	// TODO: these should be 2 separate tests "passthrough" and "solo/standalone/whatever"
	// We don't use the request for anything other than testing pass though.
	// Options not set by it should be picked up from the environment instead.

	var (
		request, _ = cmds.NewRequest(ctx, nil, nil, nil, nil, &cmds.Command{})
		types      = typeParsers()
	)
	for _, test := range []struct {
		name    string
		sources []parameters.SetFunc
	}{
		{
			name: "solo source",
			sources: []parameters.SetFunc{
				parameters.SettingsFromEnvironment(),
			},
		},
		{
			name: "source passthrough",
			sources: []parameters.SetFunc{
				parameters.SettingsFromCmds(request),
				parameters.SettingsFromEnvironment(),
			},
		},
	} {
		sources := test.sources
		t.Run(test.name, func(t *testing.T) {
			if err := parameters.Parse(ctx, settings, sources, types...); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(wantSettings, settings) {
				t.Fatalf("settings field values do not match input values"+
					"\n\tgot: %#v"+
					"\n\twant: %#v",
					settings, wantSettings)
			}
		})
	}

	t.Run("cancel context", func(t *testing.T) {
		expectedErr := context.Canceled
		testContext, testCancel := context.WithCancel(ctx)
		testCancel()
		var (
			sources = []parameters.SetFunc{parameters.SettingsFromEnvironment()}
			err     = parameters.Parse(testContext, settings, sources)
		)
		if !errors.Is(err, expectedErr) {
			t.Fatalf("error value does not match"+
				"\n\tgot: %v"+
				"\n\twant: %v",
				err, expectedErr,
			)
		}
	})
}

func testSettingsToEnv(t *testing.T, set parameters.Settings) {
	t.Helper()
	var (
		params        = set.Parameters()
		publicFields  = make([]reflect.StructField, 0, len(params))
		settingsValue = reflect.ValueOf(set).Elem()
		fields        = reflect.VisibleFields(settingsValue.Type())
	)

	for _, field := range fields {
		if !field.IsExported() {
			continue
		}
		publicFields = append(publicFields, field)
	}

	for i, param := range params {
		field := publicFields[i]
		if field.Type.Kind() == reflect.Struct {
			continue
		}
		var (
			fieldIndex = field.Index
			key        = param.Name(parameters.Environment)
			value      = settingsValue.FieldByIndex(fieldIndex).Interface()
		)
		if strs, ok := value.([]string); ok {
			value = testStringsToCSV(t, strs)
		}
		osErr := os.Setenv(key, fmt.Sprintf("%v", value))
		if osErr != nil {
			t.Fatalf("failed to set environment %q: %s",
				key, osErr)
		}
	}
}

func testStringsToCSV(t *testing.T, strs []string) string {
	t.Helper()
	var (
		strBld = new(strings.Builder)
		csvWr  = csv.NewWriter(strBld)
	)
	if err := csvWr.Write(strs); err != nil &&
		!errors.Is(err, io.EOF) {
		t.Error(err)
	}
	csvWr.Flush()
	if err := csvWr.Error(); err != nil {
		t.Error(err)
	}
	return strBld.String()
}
