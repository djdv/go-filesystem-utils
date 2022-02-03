package parameters_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

func testArguments(t *testing.T) {
	t.Parallel()
	t.Run("invalid", testArgumentsInvalid)
	t.Run("valid", testArgumentsValid)
}

func testArgumentsInvalid(t *testing.T) {
	t.Parallel()
	t.Run("cancel", testArgumentsCancel)
	t.Run("types", testArgumentsInvalidTypes)
	t.Run("parser fails", testArgumentsParserFail)
	t.Run("conflicting struct<->argument types", testArgumentsBadTypes)
	t.Run("bad sources", testParemeterInvalidArgs)
}

func testArgumentsValid(t *testing.T) {
	t.Parallel()
	t.Run("noop", testArgumentsNoop)
	t.Run("flat", testArgumentsFlat)
	t.Run("vector", testArgumentsVector)
	t.Run("compound", testArgumentsCompound)
	t.Run("embedded", testArgumentsEmbedded)
}

func testArgumentsNoop(t *testing.T) {
	t.Parallel()
	// This test will only show up in tracing, like the test coverage report.
	// It's purpose is to make sure `Parse` breaks out early when it can/should.
	// I.e. When the request has built-in cmds-lib options, but no other values.
	var (
		ctx          = context.Background()
		request, err = cmds.NewRequest(ctx, nil,
			cmds.OptMap{cmds.EncLong: "text"},
			nil, nil, &cmds.Command{})
		sources = []parameters.SetFunc{parameters.SettingsFromCmds(request)}
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := parameters.Parse(ctx, new(testFlatSettings), sources); err != nil {
		t.Fatal(err)
	}
}

func testArgumentsFlat(t *testing.T) {
	t.Parallel()
	var (
		ctx          = context.Background()
		wantSettings = new(testFlatSettings)
		options      = nonzeroOptionSetter(wantSettings)
		request, _   = cmds.NewRequest(ctx, nil, options,
			nil, nil, &cmds.Command{},
		)
	)
	nonzeroValueSetter(wantSettings)

	var (
		gotSettings = new(testFlatSettings)
		sources     = []parameters.SetFunc{
			parameters.SettingsFromCmds(request),
		}
		err = parameters.Parse(ctx, gotSettings, sources)
	)
	if err != nil {
		t.Error(err)
	}

	testCompareSettingsStructs(t, gotSettings, wantSettings)
}

func testArgumentsVector(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name           string
		emptySet, want parameters.Settings
		parsers        []parameters.TypeParser
	}{
		{
			name:     "builtin",
			emptySet: new(testVectorSettings),
			want: &testVectorSettings{
				Slice: []bool{true, false, true, false},
				Array: [8]bool{false, true, false, true},
			},
		},
		{
			name:     "external",
			emptySet: new(testVectorExternalSettings),
			want: &testVectorExternalSettings{
				Slice: []multiaddr.Multiaddr{
					multiaddr.StringCast("/tcp/1"),
					multiaddr.StringCast("/udp/2"),
				},
				Array: [2]multiaddr.Multiaddr{
					multiaddr.StringCast("/ip4/0.0.0.3"),
					multiaddr.StringCast("/dns/localhost"),
				},
			},
			parsers: typeParsers(),
		},
	} {
		var (
			got     = test.emptySet
			want    = test.want
			options = optionFromSettings(want)
			parsers = test.parsers
		)
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			t.Run("values", func(t *testing.T) {
				t.Parallel()
				var (
					request, _ = cmds.NewRequest(ctx, nil, options,
						nil, nil, &cmds.Command{},
					)
					sources = []parameters.SetFunc{
						parameters.SettingsFromCmds(request),
					}
					err = parameters.Parse(ctx, got, sources, parsers...)
				)
				if err != nil {
					t.Error(err)
				}
				testCompareSettingsStructs(t, got, want)
			})
			t.Run("strings", func(t *testing.T) {
				t.Parallel()
				var (
					stringOptions = func() cmds.OptMap {
						stringOptions := make(cmds.OptMap, len(options))
						for k, v := range options {
							stringOptions[k] = strings.Fields(
								strings.Trim(fmt.Sprint(v), "[]"))
						}
						return stringOptions
					}()
					request, _ = cmds.NewRequest(ctx, nil, stringOptions,
						nil, nil, &cmds.Command{},
					)
					sources = []parameters.SetFunc{
						parameters.SettingsFromCmds(request),
					}
					err = parameters.Parse(ctx, got, sources, parsers...)
				)
				if err != nil {
					t.Error(err)
				}
				testCompareSettingsStructs(t, got, want)
			})
		})
	}
}

func testArgumentsCompound(t *testing.T) {
	t.Parallel()
	var (
		ctx           = context.Background()
		compoundValue = compoundValue{
			A: 1,
			B: 2,
		}
		wantSettings = &testCompoundSettings{
			CompoundValue: compoundValue,
		}
		params  = wantSettings.Parameters()
		options = cmds.OptMap{
			params[0].Name(parameters.CommandLine): compoundValue,
		}
		request, _ = cmds.NewRequest(ctx, nil, options,
			nil, nil, &cmds.Command{},
		)
	)

	var (
		gotSettings = new(testCompoundSettings)
		sources     = []parameters.SetFunc{
			parameters.SettingsFromCmds(request),
		}
		err = parameters.Parse(ctx, gotSettings, sources)
	)
	if err != nil {
		t.Error(err)
	}

	testCompareSettingsStructs(t, gotSettings, wantSettings)
}

func testArgumentsEmbedded(t *testing.T) {
	t.Parallel()
	var (
		ctx           = context.Background()
		compoundValue = compoundValue{
			A: 1,
			B: 2,
		}
		wantSettings = &testSubPkgSettings{
			testCompoundSettings: testCompoundSettings{
				CompoundValue: compoundValue,
			},
			testVectorSettings: testVectorSettings{
				Slice: []bool{true, false, true, false},
				Array: [8]bool{false, true, false, true},
			},
			C: 3,
			D: 4,
		}
		options        = nonzeroOptionSetter(&wantSettings.testFlatSettings)
		compoundParams = wantSettings.testCompoundSettings.Parameters()
		vectorParams   = wantSettings.testVectorSettings.Parameters()
		toStrings      = func(vector []bool) []string {
			return strings.Fields(strings.Trim(fmt.Sprint(vector), "[]"))
		}
		extraOptions = cmds.OptMap{
			compoundParams[0].Name(parameters.CommandLine): compoundValue,
			vectorParams[0].Name(parameters.CommandLine):   toStrings(wantSettings.Slice),
			vectorParams[1].Name(parameters.CommandLine):   toStrings(wantSettings.Array[:]),
			"c": fmt.Sprint(wantSettings.C),
			"d": fmt.Sprint(wantSettings.D),
		}
	)
	for k, v := range extraOptions {
		options[k] = v
	}

	request, _ := cmds.NewRequest(ctx, nil, options,
		nil, nil, &cmds.Command{},
	)
	nonzeroValueSetter(&wantSettings.testFlatSettings)

	var (
		gotSettings = new(testSubPkgSettings)
		sources     = []parameters.SetFunc{
			parameters.SettingsFromCmds(request),
		}
		err = parameters.Parse(ctx, gotSettings, sources)
	)
	if err != nil {
		t.Error(err)
	}

	testCompareSettingsStructs(t, gotSettings, wantSettings)
}

func testArgumentsCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var (
		request, err = cmds.NewRequest(context.Background(), nil,
			nil, nil, nil, &cmds.Command{})
		sources = []parameters.SetFunc{parameters.SettingsFromCmds(request)}
	)
	if err != nil {
		t.Fatal(err)
	}

	expectedErr := context.Canceled
	err = parameters.Parse(ctx, new(testFlatSettings), sources)
	if !errors.Is(err, expectedErr) {
		t.Errorf("error value does not match"+
			"\n\twanted: %v"+
			"\n\tgot: %v",
			expectedErr, err,
		)
	}
}

func testArgumentsInvalidTypes(t *testing.T) {
	t.Parallel()
	var (
		ctx        = context.Background()
		params     = invalidParamSet()
		request, _ = cmds.NewRequest(ctx, nil,
			cmds.OptMap{
				params[0].Name(parameters.CommandLine): true,
				params[1].Name(parameters.CommandLine): 42,
			},
			nil, nil, &cmds.Command{})
	)

	for _, test := range invalidInterfaces {
		var (
			testName = test.name
			settings = test.settingsIntf
			failMsg  = test.nonErrorMessage
		)
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			sources := requestAndEnvSources(request)
			if err := parameters.Parse(ctx, settings, sources); err == nil {
				t.Error("expected an error but did not receive one - ",
					failMsg)
			} else {
				t.Log(err)
			}
		})
	}
}

func testArgumentsParserFail(t *testing.T) {
	t.Parallel()
	var (
		ctx         = context.Background()
		expectedErr = errors.New("refusing to parse")
		badParser   = func(argument string) (value interface{}, _ error) {
			return nil, expectedErr
		}
		settings   = new(testPkgSettings)
		params     = settings.Parameters()
		request, _ = cmds.NewRequest(ctx, nil,
			cmds.OptMap{
				params[0].Name(parameters.CommandLine): "",
			},
			nil, nil, &cmds.Command{})

		sources     = requestAndEnvSources(request)
		typeHandler = parameters.TypeParser{
			Type:      reflect.TypeOf((*multiaddr.Multiaddr)(nil)).Elem(),
			ParseFunc: badParser,
		}
		err = parameters.Parse(ctx, settings, sources, typeHandler)
	)
	if err == nil {
		t.Error("expected error but got none (non-parseable special values)")
	}
}

func testArgumentsBadTypes(t *testing.T) {
	t.Parallel()
	var (
		ctx        = context.Background()
		settings   = new(testPkgSettings)
		params     = settings.Parameters()
		badValue   = true
		request, _ = cmds.NewRequest(ctx, nil,
			cmds.OptMap{
				params[1].Name(parameters.CommandLine): badValue,
				params[2].Name(parameters.CommandLine): badValue,
				params[3].Name(parameters.CommandLine): badValue,
			},
			nil, nil, &cmds.Command{})
		sources = requestAndEnvSources(request)
		err     = parameters.Parse(ctx, settings, sources)
	)
	if err == nil {
		t.Error("expected error but got none (value types mismatch)")
	}
}
