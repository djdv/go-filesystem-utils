package parameters_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

// Below we define a few abstract `Parameters`
// that `Settings` interfaces may use in their particular list of parameters.
// In practice these should be exported with your pkg, so that other pkgs may use them;
// but in these tests they are not since they're used exclusively for testing.
//
// By default, their names are derived from the name of the function that returns them.
// But this can be changed via an option passed to NewParameter.
func testParam() parameters.Parameter {
	return parameters.NewParameter(
		"Test parameter's description text.",
	)
}

func testParam2() parameters.Parameter {
	return parameters.NewParameter(
		"I'm used to test the parameter library.",
	)
}

func simpleParam() parameters.Parameter {
	return parameters.NewParameter(
		"I'm a simple parameter used to test simple assignments.",
	)
}

func complexParam() parameters.Parameter {
	return parameters.NewParameter(
		"I'm a complex parameter used to test advanced assignments.",
	)
}

func nestedComplexParam() parameters.Parameter {
	return parameters.NewParameter(
		"I'm a complex parameter used to test recursive assignments.",
	)
}

// This Settings struct may be used on its own,
// but is also intended to be inherited by others.
// As if it was declared in another pkg as `root.Settings`,
// and utilized here.
// Since this is a test pkg, everything is in the same place.
type TestRootSettings struct {
	unrelatedField1 int8
	unrelatedField2 int16
	// Notice the struct tag. This indicates to the library
	// that this is the location where settings fields start.
	// There is no end marker. Parsers use the parameter list
	// returned by the `Parameters()` method of the `Settings` interface,
	// to determine when to stop parsing fields.
	TestField  bool `settings:"arguments"`
	TestField2 int
}

// Give the root settings struct a list of parameters,
// canonizing it as a `Settings` interface.
// The order of this slice must follow
// the order of fields in the `Settings` underlying struct.
//
// Note that just because we satisfy the interface,
// does not mean the struct is declared properly.
// This is only an additional (but weak) layer of type safety.
// The compiler cannot otherwise help us,
// but the library will provide descriptive errors immediately at runtime.
func (*TestRootSettings) Parameters() parameters.Parameters {
	return parameters.Parameters{
		testParam(),  // Associated with `TestField`
		testParam2(), // Associated with `TestField2`, etc.
	}
}

type (
	testComplexType struct {
		A uint
		B int64
	}

	testVeryComplexType struct {
		testComplexType
		C uint64
		D float64
		E string
		F multiaddr.Multiaddr
		G []rune
	}

	// Settings intended to be relevant to functions in this pkg,
	// or inherited by another pkg.
	testSettings struct {
		// Unrelated fields before settings should have no effects.
		unrelatedField3 int32
		// The settings struct tag should be detected even when embedded.
		TestRootSettings
		// Complex types should be assignable (but must not be embedded).
		Complex testComplexType
		// Simple types should just work.
		Simple int16
		// Nested complex types are just as valid.
		VeryComplex testVeryComplexType
		// Unrelated fields after settings should have no effects.
		unrelatedField4 int64
	}
)

// Associate the pkg settings struct with a list of parameters.
func (*testSettings) Parameters() parameters.Parameters {
	var (
		// Inherit root parameters from the root Settings type/interface.
		rootParams = (*TestRootSettings)(nil).Parameters()
		// These parameters pertain to our/this Settings type.
		pkgParams = []parameters.Parameter{
			complexParam(),
			simpleParam(),
			nestedComplexParam(),
		}
	)
	// We concatenate the lists of parameters together to form a super-set.
	// As Go does for the struct itself.
	// I.e. As Go expands embedded struct super-fields
	// into the sub-struct's own fields;
	// we expand the super-parameters into our own parameters.
	return append(rootParams, pkgParams...)
}

func TestArguments(t *testing.T) {
	var (
		ctx = context.Background()

		// Values to test. (Assign and compare)
		paramValue   = true
		param2Value  = 42
		complexValue = testComplexType{
			A: 1,
			B: -2,
		}
		simpleValue      = int16(16)
		veryComplexValue = testVeryComplexType{
			testComplexType: complexValue,
			C:               64,
			D:               64.0,
			E:               "sixty four",
			F:               multiaddr.StringCast("/ip4/0.6.4.0"),
			G:               []rune{'6', '4'},
		}

		// Create a cmds-lib request, utilizing the keys provided
		// by the parameters themselves in order to set option values.
		request, _ = cmds.NewRequest(ctx, nil,
			cmds.OptMap{
				testParam().CommandLine():          paramValue,
				testParam2().CommandLine():         param2Value,
				complexParam().CommandLine():       complexValue,
				simpleParam().CommandLine():        simpleValue,
				nestedComplexParam().CommandLine(): veryComplexValue,
			},
			nil, nil, &cmds.Command{})
		// Instantiate the settings struct we need for this function.
		settings = new(testSettings)
		// Provide a list of sources where we might find values.
		unsetArgs, errs = parameters.ParseSettings(ctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
		// Actually start the processing of the sources,
		// returning a slice of arguments that were not set (ignored here),
		// and an error if encountered.
		_, err = parameters.AccumulateArgs(ctx, unsetArgs, errs)
	)
	if err != nil {
		t.Fatal(err)
	}

	// Validation.
	validSettings := &testSettings{
		TestRootSettings: TestRootSettings{
			TestField:  paramValue,
			TestField2: param2Value,
		},
		Complex:     complexValue,
		Simple:      simpleValue,
		VeryComplex: veryComplexValue,
	}
	if !reflect.DeepEqual(validSettings, settings) {
		t.Fatalf("settings field values do not match input values\n\twanted:\n\t%#v\n\tgot:\n\t%#v",
			validSettings, settings)
	}

	t.Run("skip cmds", func(t *testing.T) {
		// This will only show up in tracing, like the test coverage report.
		// When we determine no parameter options are in the cmds request,
		// we skip parsing the request further.
		request.Options = nil
		request, err := cmds.NewRequest(ctx, nil,
			cmds.OptMap{cmds.EncLong: "text"},
			nil, nil, &cmds.Command{})
		if err != nil {
			t.Fatal(err)
		}
		unsetArgs, errs := parameters.ParseSettings(ctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
		if _, err := parameters.AccumulateArgs(ctx, unsetArgs, errs); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("cancel context", func(t *testing.T) {
		expectedErr := context.Canceled
		t.Run("parse", func(t *testing.T) {
			testContext, testCancel := context.WithCancel(ctx)
			testCancel()
			unsetArgs, errs = parameters.ParseSettings(testContext, settings,
				parameters.SettingsFromCmds(request),
				parameters.SettingsFromEnvironment(),
			)
			_, err := parameters.AccumulateArgs(testContext, unsetArgs, errs)
			if !errors.Is(err, expectedErr) {
				t.Fatalf("error value does not match"+
					"\n\twanted: %v"+
					"\n\tgot: %v",
					expectedErr, err,
				)
			}
		})
		t.Run("accumulate", func(t *testing.T) {
			var (
				testContext, testCancel = context.WithCancel(ctx)
				unsetArgs, errs         = parameters.ParseSettings(testContext, settings,
					parameters.SettingsFromCmds(request),
					parameters.SettingsFromEnvironment(),
				)
			)
			testCancel()
			_, err := parameters.AccumulateArgs(testContext, unsetArgs, errs)
			if !errors.Is(err, expectedErr) {
				t.Fatalf("error value does not match"+
					"\n\twanted: %v"+
					"\n\tgot: %v",
					expectedErr, err,
				)
			}
		})
	})
}

func TestEnvironment(t *testing.T) {
	var (
		ctx      = context.Background()
		settings = new(testOptionsSettings)
		params   = settings.Parameters()
		clearEnv = func() {
			for _, param := range params {
				key := param.Environment()
				if osErr := os.Unsetenv(key); osErr != nil {
					t.Errorf("failed to unset environment %q: %s",
						key, osErr)
				}
			}
		}
	)

	// Make sure env is clear before and after the test.
	clearEnv()
	defer clearEnv()

	var (
		validSettings = &testOptionsSettings{
			TestRootSettings: TestRootSettings{
				TestField:  true,
				TestField2: 1,
			},
			A: 2,
			B: 3,
			C: 4,
			D: 5,
			E: "six",
			F: multiaddr.StringCast("/ip4/0.0.0.7"),
			G: []string{"8", "9"},
		}
		// XXX: lazy magic - not particular safe
		insertInEnv = func(set parameters.Settings, offset int, params []parameters.Parameter) {
			var (
				setValue = reflect.ValueOf(set).Elem()
				paramEnd = len(params) - 1
			)
			for pi, si := 0, offset; pi <= paramEnd; pi, si = pi+1, si+1 {
				value := setValue.Field(si).Interface()
				if strs, ok := value.([]string); ok {
					value = strings.Join(strs, ",") // [] => CSV
				}
				key := params[pi].Environment()
				osErr := os.Setenv(key, fmt.Sprintf("%v", value))
				if osErr != nil {
					t.Fatalf("failed to set environment %q: %s",
						key, osErr)
				}
			}
		}
	)
	// Populate the env with our expected data.
	var (
		rootParams = validSettings.TestRootSettings.Parameters()
		pkgParams  = validSettings.Parameters()[len(rootParams):]
	)
	const (
		// XXX: magic because lazy tests, don't mimick this.
		unrelatedRootPadding = 2
		paddingAndRoot       = 2
	)
	insertInEnv(&validSettings.TestRootSettings, unrelatedRootPadding, rootParams)
	insertInEnv(validSettings, paddingAndRoot, pkgParams)

	var (
		// We don't use the request for anything other than testing pass though.
		// Options not set by it, should be picked up from the environment.
		request, _      = cmds.NewRequest(ctx, nil, nil, nil, nil, &cmds.Command{})
		unsetArgs, errs = parameters.ParseSettings(ctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
		_, err = parameters.AccumulateArgs(ctx, unsetArgs, errs)
	)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(validSettings, settings) {
		t.Fatalf("settings field values do not match input values\n\twanted:\n\t%#v\n\tgot:\n\t%#v",
			validSettings, settings)
	}
	t.Run("cancel context", func(t *testing.T) {
		expectedErr := context.Canceled
		t.Run("parse", func(t *testing.T) {
			testContext, testCancel := context.WithCancel(ctx)
			testCancel()
			unsetArgs, errs = parameters.ParseSettings(testContext, settings,
				parameters.SettingsFromEnvironment(),
			)
			_, err := parameters.AccumulateArgs(testContext, unsetArgs, errs)
			if !errors.Is(err, expectedErr) {
				t.Fatalf("error value does not match"+
					"\n\twanted: %v"+
					"\n\tgot: %v",
					expectedErr, err,
				)
			}
		})
		t.Run("accumulate", func(t *testing.T) {
			var (
				testContext, testCancel = context.WithCancel(ctx)
				unsetArgs, errs         = parameters.ParseSettings(testContext, settings,
					parameters.SettingsFromEnvironment(),
				)
			)
			testCancel()
			_, err := parameters.AccumulateArgs(testContext, unsetArgs, errs)
			if !errors.Is(err, expectedErr) {
				t.Fatalf("error value does not match"+
					"\n\twanted: %v"+
					"\n\tgot: %v",
					expectedErr, err,
				)
			}
		})
	})
}

// Below just tests various invalid declarations/combinations.
type (
	testBadSettingsTagless struct {
		TestField  bool
		TestField2 bool
	}
	testBadSettingsNonStandardTag struct {
		TestField  bool `settings:"""arguments"""` // it's like I'm really using Batch
		TestField2 bool
	}
	testBadSettingsShort struct {
		TestField bool `settings:"arguments"`
	}
	testBadSettingsWrongType struct {
		TestField  bool `settings:"arguments"`
		TestField2 bool
	}
	testBadSettingsUnassignable struct {
		testField  bool `settings:"arguments"`
		testField2 bool
	}
	// NOTE: This could be made to work,
	// but currently there's no obvious need for it.
	testNotAStruct bool
)

func invalidParamSet() []parameters.Parameter {
	return parameters.Parameters{
		testParam(),
		testParam2(),
	}
}

func (*testBadSettingsTagless) Parameters() parameters.Parameters        { return invalidParamSet() }
func (*testBadSettingsNonStandardTag) Parameters() parameters.Parameters { return invalidParamSet() }
func (*testBadSettingsShort) Parameters() parameters.Parameters          { return invalidParamSet() }
func (*testBadSettingsWrongType) Parameters() parameters.Parameters      { return invalidParamSet() }
func (*testBadSettingsUnassignable) Parameters() parameters.Parameters   { return invalidParamSet() }
func (testNotAStruct) Parameters() parameters.Parameters                 { return invalidParamSet() }

type invalidInterfaceSet struct {
	name            string
	settingsIntf    parameters.Settings
	nonErrorMessage string
}

var invalidInterfaces = []invalidInterfaceSet{
	{
		"tagless",
		new(testBadSettingsTagless),
		"struct has no tag",
	},
	{
		"malformed tag",
		new(testBadSettingsNonStandardTag),
		"struct has non-standard tag",
	},
	{
		"fewer fields",
		new(testBadSettingsShort),
		"struct has fewer fields than parameters",
	},
	{
		"unassignable fields",
		new(testBadSettingsUnassignable),
		"struct fields are not assignable by reflection",
	},
	{
		"invalid concrete type",
		new(testNotAStruct),
		"this Settings interface is not a struct",
	},
	{
		"invalid concrete type",
		testNotAStruct(true),
		"this Settings interface is not a pointer",
	},
}

func TestInvalidArguments(t *testing.T) {
	var (
		ctx        = context.Background()
		request, _ = cmds.NewRequest(ctx, nil,
			cmds.OptMap{
				testParam().CommandLine():  true,
				testParam2().CommandLine(): 42,
			},
			nil, nil, &cmds.Command{})
	)

	argumentTests := append([]invalidInterfaceSet{
		{
			"mismatched types",
			new(testBadSettingsWrongType),
			"struct fields have different types than the source provides",
		}},
		invalidInterfaces...,
	)

	for _, test := range argumentTests {
		var (
			testName = test.name
			settings = test.settingsIntf
			failMsg  = test.nonErrorMessage
		)
		t.Run(testName, func(t *testing.T) {
			unsetArgs, errs := parameters.ParseSettings(ctx, settings,
				parameters.SettingsFromCmds(request),
				parameters.SettingsFromEnvironment(),
			)
			if _, err := parameters.AccumulateArgs(ctx, unsetArgs, errs); err == nil {
				t.Error("expected an error but did not receive one - ",
					failMsg)
			} else {
				t.Log(err)
			}
		})
	}
}

// The cmds-lib supports a subset of types.
// non-built-in types (like multiaddr) need special handling within the library
// to gain cmds-lib generation support.
// It may be possible to allow these to be defined and passed at runtime
// but this is not currently supported as it is not currently needed.
type testOptionsSettings struct {
	unrelatedField1 bool
	TestRootSettings
	A               uint
	B               int64
	C               uint64
	D               float64
	E               string
	F               multiaddr.Multiaddr
	G               []string
	unrelatedField2 bool
}

// XXX: Don't imitate this in any real Go code. This is a lazy C-ism.
const testOptionParamCount = 'G' - 'A' + 1

func (*testOptionsSettings) Parameters() parameters.Parameters {
	var (
		rootParams = (*TestRootSettings)(nil).Parameters()
		pkgParams  = make([]parameters.Parameter, testOptionParamCount)
	)
	for i := range pkgParams {
		pkgParams[i] = parameters.NewParameter(
			fmt.Sprintf("auto generated test param %d", i),
			parameters.WithNamespace("test"),
			parameters.WithName(fmt.Sprintf("param%d%c", i, 'A'+i)),
			parameters.WithEnvironmentPrefix("TEST"),
		)
	}
	return append(rootParams, pkgParams...)
}

func TestOptions(t *testing.T) {
	for _, test := range []struct {
		name         string
		settingsIntf parameters.Settings
		count        int
	}{
		{
			"root",
			new(TestRootSettings),
			2,
		},
		{
			"pkg",
			new(testOptionsSettings),
			testOptionParamCount,
		},
	} {
		var (
			testName      = test.name
			settings      = test.settingsIntf
			expectedCount = test.count
		)
		t.Run(testName, func(t *testing.T) {
			opts := parameters.CmdsOptionsFrom(settings)
			if optLen := len(opts); expectedCount != optLen {
				optStrings := make([]string, optLen)
				for i, opt := range opts {
					optStrings[i] = opt.Name()
				}
				t.Fatalf("settings options do not match expected count"+
					"\n\twanted: %d"+
					"\n\tgot: {%d}[%s]",
					expectedCount, optLen, strings.Join(optStrings, ", "),
				)
			}
		})
	}
}

func TestInvalidOptions(t *testing.T) {
	for _, test := range invalidInterfaces {
		var (
			testName = test.name
			settings = test.settingsIntf
			failMsg  = test.nonErrorMessage
		)
		t.Run(testName, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Error("expected to panic but did not - ",
						failMsg)
				} else {
					t.Log("recovered from panic:\n\t", r)
				}
			}()
			parameters.CmdsOptionsFrom(settings)
		})
	}
	t.Run("from closure without info", func(t *testing.T) {
		failMsg := "non-pkg level parameters need to provide their own namespace and name via options"
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected to panic but did not - ",
					failMsg)
			} else {
				t.Log("recovered from panic:\n\t", r)
			}
		}()
		helloIAmClosure := func() {
			parameters.NewParameter("oh no")
		}
		helloIAmClosure()
	})
	t.Run("from lambda without info", func(t *testing.T) {
		failMsg := "non-pkg level parameters need to provide their own namespace and name via options"
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected to panic but did not - ",
					failMsg)
			} else {
				t.Log("recovered from panic:\n\t", r)
			}
		}()
		func() {
			parameters.NewParameter("おの")
		}()
	})
}
