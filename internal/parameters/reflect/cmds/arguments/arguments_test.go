package arguments_test

import (
	"context"
	"encoding/csv"
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
	if err := parameters.Parse(ctx, new(flatSettings), sources); err != nil {
		t.Fatal(err)
	}
}

func testArgumentsFlat(t *testing.T) {
	t.Parallel()
	var (
		ctx          = context.Background()
		wantSettings = new(flatSettings)
		options      = nonzeroOptionSetter(wantSettings)
		request, _   = cmds.NewRequest(ctx, nil, options,
			nil, nil, &cmds.Command{},
		)
	)
	nonzeroValueSetter(wantSettings)

	var (
		gotSettings = new(flatSettings)
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
			name:     "builtin handlers",
			emptySet: new(vectorSettings),
			want: &vectorSettings{
				Slice: []bool{true, false, true, false},
				Array: [8]bool{false, true, false, true},
			},
		},
		{
			name:     "external handlers",
			emptySet: new(vectorExternalHandlerSettings),
			want: &vectorExternalHandlerSettings{
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
			parsers = test.parsers
		)
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			t.Run("values", func(t *testing.T) {
				t.Parallel()
				testVectorValues(t, got, want, parsers)
			})
			t.Run("strings", func(t *testing.T) {
				t.Parallel()
				testVectorStrings(t, got, want, parsers)
			})
		})
	}
}

func testVectorValues(t *testing.T,
	got, want parameters.Settings, parsers []parameters.TypeParser,
) {
	var (
		ctx        = context.Background()
		options    = optionsFromSettings(want)
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
}

func testVectorStrings(t *testing.T,
	empty, want parameters.Settings, parsers []parameters.TypeParser,
) {
	var (
		ctx           = context.Background()
		options       = optionsFromSettings(want)
		stringOptions = func() cmds.OptMap {
			stringOptions := make(cmds.OptMap, len(options))
			for k, v := range options {
				// t.Log("dbg 4:", strings.Fields(
				//	strings.Trim(fmt.Sprint(v), "[]")))

				// stringOptions[k] = strings.Fields(
				//	strings.Trim(fmt.Sprint(v), "[]"))
				var (
					goString         = fmt.Sprint(v)
					syntaxlessString = strings.Trim(goString, "[]")
					argumentStrings  = strings.Fields(syntaxlessString)

					builder   strings.Builder
					csvWriter = csv.NewWriter(&builder)
				)
				if err := csvWriter.Write(argumentStrings); err != nil {
					t.Fatal(err)
				}
				csvWriter.Flush()
				if err := csvWriter.Error(); err != nil {
					t.Fatal(err)
				}
				stringOptions[k] = builder.String()
			}
			return stringOptions
		}()
		// FIXME: NewRequest will not parse csv strings
		// since the Command is nil
		// (it gets the typedef from the cmd.Options[name] map defined on it)
		// i.e. we need to call cmds.makeStringOpts or whatever first

		root = &cmds.Command{
			Options: parameters.MustMakeCmdsOptions(empty),
		}
		request, _ = cmds.NewRequest(ctx, nil, stringOptions,
			nil, nil, root,
		)
		sources = []parameters.SetFunc{
			parameters.SettingsFromCmds(request),
		}
		err = parameters.Parse(ctx, empty, sources, parsers...)
	)
	if err != nil {
		t.Error(err)
	}
	got := empty // (Formerly empty)

	{ // DBG
		opts := stringOptions
		optDefs, err := root.GetOptions(nil)
		options := make(cmds.OptMap)

		if err != nil {
			t.Fatal(err)
		}
		for k, v := range opts {
			options[k] = v
		}

		for k, v := range opts {
			opt, ok := optDefs[k]
			if !ok {
				continue
			}

			kind := reflect.TypeOf(v).Kind()
			if kind != opt.Type() {
				t.Log("internal b1")
				if opt.Type() == cmds.Strings {
					t.Log("internal b2")
					if _, ok := v.([]string); !ok {
						t.Log("internal b3")
						t.Fatal(
							fmt.Errorf("option %q should be type %q, but got type %q",
								k, opt.Type().String(), kind.String()))
					}
				} else {
					t.Log("internal b4")
					str, ok := v.(string)
					if !ok {
						t.Fatal(fmt.Errorf("option %q should be type %q, but got type %q",
							k, opt.Type().String(), kind.String()))
					}

					val, err := opt.Parse(str)
					if err != nil {
						value := fmt.Sprintf("value %q", v)
						if len(str) == 0 {
							value = "empty value"
						}
						t.Fatal(fmt.Errorf("could not convert %s to type %q (for option %q)",
							value, opt.Type().String(), "-"+k))
					}
					options[k] = val
				}
			}

			for _, name := range opt.Names() {
				if _, ok := options[name]; name != k && ok {
					t.Fatal(fmt.Errorf("duplicate command options were provided (%q and %q)",
						k, name))
				}
			}
		}
	}

	testCompareSettingsStructs(t, got, want)
}

func testArgumentsCompound(t *testing.T) {
	t.Parallel()
	var (
		ctx           = context.Background()
		compoundValue = structType{
			A: 1,
			B: 2,
		}
		wantSettings = &compoundSettings{
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
		gotSettings = new(compoundSettings)
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
		ctx = context.Background()
		/*
			compoundValue = testStructType{
				A: 1,
				B: 2,
			}
		*/
		wantSettings = &subPkgSettings{
			//testCompoundSettings: testCompoundSettings{
			//	CompoundValue: compoundValue,
			//},
			vectorSettings: vectorSettings{
				Slice: []bool{true, false, true, false},
				Array: [8]bool{false, true, false, true},
			},
			C: 3,
			D: 4,
		}
		options = nonzeroOptionSetter(&wantSettings.flatSettings)
		// compoundParams = wantSettings.testCompoundSettings.Parameters()
		vectorParams = wantSettings.vectorSettings.Parameters()
		toStrings    = func(vector []bool) []string {
			return strings.Fields(strings.Trim(fmt.Sprint(vector), "[]"))
		}
		extraOptions = cmds.OptMap{
			// compoundParams[0].Name(parameters.CommandLine): compoundValue,
			vectorParams[0].Name(parameters.CommandLine): toStrings(wantSettings.Slice),
			vectorParams[1].Name(parameters.CommandLine): toStrings(wantSettings.Array[:]),
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
	nonzeroValueSetter(&wantSettings.flatSettings)

	var (
		gotSettings = new(subPkgSettings)
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
	err = parameters.Parse(ctx, new(flatSettings), sources)
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
		ctx       = context.Background()
		wantErr   = errors.New("refusing to parse")
		badParser = func(argument string) (value interface{}, _ error) {
			return nil, wantErr
		}
		settings   = new(externalSettings)
		params     = settings.Parameters()
		request, _ = cmds.NewRequest(ctx, nil,
			cmds.OptMap{
				params[0].Name(parameters.CommandLine): "",
			},
			nil, nil, &cmds.Command{})

		sources      = requestAndEnvSources(request)
		externalType = reflect.TypeOf((*externalType)(nil)).Elem()
		typeHandler  = parameters.TypeParser{
			Type:      externalType,
			ParseFunc: badParser,
		}
		gotErr = parameters.Parse(ctx, settings, sources, typeHandler)
	)
	if !errors.Is(gotErr, wantErr) {
		t.Error("expected error but got none (non-parseable special values)")
	}
}

func testArgumentsBadTypes(t *testing.T) {
	t.Parallel()
	var (
		ctx        = context.Background()
		settings   = new(pkgSettings)
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
