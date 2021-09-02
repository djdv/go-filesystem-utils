package parameters_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

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

// The Parameter constructor can take in a series of options.
func testParam2() parameters.Parameter {
	return parameters.NewParameter(
		"I'm used to test the parameter library.",
		parameters.WithRootNamespace(),
		parameters.WithName("Something Different"), // --something-different
		parameters.WithEnvironmentPrefix("t"),      // T_SOMETHING_DIFFERENT
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

// You can imagine these parameters being part of another pkg.
// They're in the same file just for testing.
func embeddedParam1() parameters.Parameter {
	return parameters.NewParameter(
		"I'm a parameter for a settings struct that gets embedded",
	)
}

func embeddedParam2() parameters.Parameter {
	return parameters.NewParameter(
		"I'm a parameter for a settings struct that gets embedded",
	)
}

func embeddedParam3() parameters.Parameter {
	return parameters.NewParameter(
		"I'm a parameter for a settings struct that gets embedded",
	)
}

func embeddedParam4() parameters.Parameter {
	return parameters.NewParameter(
		"I'm a parameter for a settings struct that gets embedded",
	)
}

func embeddedParam5() parameters.Parameter {
	return parameters.NewParameter(
		"I'm a parameter for a settings struct that gets embedded",
	)
}

func embeddedParam6() parameters.Parameter {
	return parameters.NewParameter(
		"I'm a parameter for a settings struct that gets embedded",
	)
}

// This is just an arbitrary struct that's not part of the parameter settings
// but is part of the setting struct. It's here just to satisfy the tests and can be ignored.
type unrelatedEmbed struct {
	pointless bool
	and       byte
	unused    int
}

// This Settings struct may be used on its own,
// but is also intended to be inherited by others.
// As if it was declared in another pkg as `root.Settings`,
// and utilized here.
// Since this is a test pkg, everything is in the same place.
type TestRootSettings struct {
	unrelatedField1 int8
	unrelatedField2 int16
	unrelatedField3 struct{}
	unrelatedEmbed
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

	testEmbed struct {
		H multiaddr.Multiaddr
		I []multiaddr.Multiaddr
		J time.Duration
		K bool
		L int
		M uint
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
		// Embedded structs should get flattened into their fields.
		// Regardless of if they're tagged or not.
		testEmbed
		// Unrelated fields after settings should have no effects.
		unrelatedField4 int64
	}
)

// Associate the embedded settings struct with a list of parameters.
func (*testEmbed) Parameters() parameters.Parameters {
	return []parameters.Parameter{
		embeddedParam1(),
		embeddedParam2(),
		embeddedParam3(),
		embeddedParam4(),
		embeddedParam5(),
		embeddedParam6(),
	}
}

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
		embeddedParams = (*testEmbed)(nil).Parameters()
	)
	// We concatenate the lists of parameters together to form a super-set.
	// As Go does for the struct itself.
	// I.e. As Go expands embedded struct super-fields
	// into the sub-struct's own fields;
	// we expand the super-parameters into our own parameters.
	return append(rootParams, append(pkgParams, embeddedParams...)...)
}

func TestArguments(t *testing.T) {
	var (
		ctx          = context.Background()
		stringMaddrs = []string{"/ip4/0.6.4.0", "/ip4/6.0.0.4", "/tcp/64"}

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
			F:               multiaddr.StringCast(stringMaddrs[0]),
			G:               []rune{'6', '4'},
		}
		embeddedValue = testEmbed{
			H: multiaddr.StringCast(stringMaddrs[0]),
			I: []multiaddr.Multiaddr{
				multiaddr.StringCast(stringMaddrs[1]),
				multiaddr.StringCast(stringMaddrs[2]),
			},
			J: time.Duration(64 * time.Second),
			K: true,
			L: 1234,
			M: 7,
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
				embeddedParam1().CommandLine():     stringMaddrs[0],
				embeddedParam2().CommandLine():     stringMaddrs[1:],
				embeddedParam3().CommandLine():     embeddedValue.J.String(),
				embeddedParam4().CommandLine():     embeddedValue.K,
				embeddedParam5().CommandLine():     embeddedValue.L,
				embeddedParam6().CommandLine():     embeddedValue.M,
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
		testEmbed:   embeddedValue,
	}
	if !reflect.DeepEqual(validSettings, settings) {
		t.Fatalf("settings field values do not match input values"+
			"\n\twanted:"+
			"\n\t%#v"+ // These long structs get their own lines.
			"\n\tgot:"+
			"\n\t%#v",
			validSettings, settings)
	}

	t.Run("direct assign", func(t *testing.T) {
		request.SetOption(embeddedParam1().CommandLine(), validSettings.testEmbed.H)
		request.SetOption(embeddedParam2().CommandLine(), validSettings.testEmbed.I)
		request.SetOption(embeddedParam3().CommandLine(), validSettings.testEmbed.J)

		settings.testEmbed.H = nil
		settings.testEmbed.I = nil
		settings.testEmbed.J = 0

		unsetArgs, errs = parameters.ParseSettings(ctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
		// Actually start the processing of the sources,
		// returning a slice of arguments that were not set (ignored here),
		// and an error if encountered.
		if _, err := parameters.AccumulateArgs(ctx, unsetArgs, errs); err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(validSettings, settings) {
			t.Fatalf("settings field values do not match input values"+
				"\n\twanted:"+
				"\n\t%#v"+ // These long structs get their own lines.
				"\n\tgot:"+
				"\n\t%#v",
				validSettings, settings)
		}
	})

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
		var (
			expectedErr = context.Canceled
			checkErr    = func(err error) {
				t.Helper()
				if !errors.Is(err, expectedErr) {
					t.Errorf("error value does not match"+
						"\n\twanted: %v"+
						"\n\tgot: %v",
						expectedErr, err,
					)
				}
			}
		)
		t.Run("parse", func(t *testing.T) {
			testContext, testCancel := context.WithCancel(ctx)
			testCancel()
			t.Run("cmds", func(t *testing.T) {
				unsetArgs, errs = parameters.ParseSettings(testContext, settings,
					parameters.SettingsFromCmds(request),
				)
				_, err := parameters.AccumulateArgs(testContext, unsetArgs, errs)
				checkErr(err)
			})
			t.Run("env", func(t *testing.T) {
				unsetArgs, errs = parameters.ParseSettings(testContext, settings,
					parameters.SettingsFromEnvironment(),
				)
				_, err := parameters.AccumulateArgs(testContext, unsetArgs, errs)
				checkErr(err)
			})
		})
		t.Run("accumulate", func(t *testing.T) {
			var (
				unsetArgs, errs = parameters.ParseSettings(ctx, settings,
					parameters.SettingsFromCmds(request),
					parameters.SettingsFromEnvironment(),
				)
				testContext, testCancel = context.WithCancel(ctx)
			)
			testCancel()
			_, err := parameters.AccumulateArgs(testContext, unsetArgs, errs)
			checkErr(err)
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
		// XXX: lazy magic - not particularly safe
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
		// XXX: magic because lazy tests, don't mimic this.
		unrelatedRootPadding = 4
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
		t.Fatalf("settings field values do not match input values"+
			"\n\twanted:"+
			"\n\t%#v"+ // These long structs get their own lines.
			"\n\tgot:"+
			"\n\t%#v",
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
	testBadSettingsUnhandledType1 struct {
		TestField  interface{} `settings:"arguments"`
		TestField2 *interface{}
	}
	// NOTE: This could be made to work,
	// but the cmds lib currently doesn't account for these.
	testBadSettingsUnhandledType2 struct {
		TestField  complex128 `settings:"arguments"`
		TestField2 complex64
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
func (*testBadSettingsUnhandledType1) Parameters() parameters.Parameters { return invalidParamSet() }
func (*testBadSettingsUnhandledType2) Parameters() parameters.Parameters { return invalidParamSet() }
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
		"uses unhandled types",
		new(testBadSettingsUnhandledType1),
		"this Settings interface contains types we don't account for",
	},
	{
		"uses unhandled types",
		new(testBadSettingsUnhandledType2),
		"this Settings interface contains types we don't account for",
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

	for _, test := range invalidInterfaces {
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
	t.Run("nil inputs", func(t *testing.T) {
		args := make(chan *parameters.Argument, 1)
		args <- (*parameters.Argument)(nil)
		close(args)
		if _, err := parameters.AccumulateArgs(ctx, args, nil); err == nil {
			t.Error("expected an error but did not receive one - " +
				"argument generator provided nil argument")
		}
	})

	t.Run("multiple errors", func(t *testing.T) {
		var (
			te1  = errors.New("test 1")
			te2  = errors.New("test 2")
			errs = make(chan error, 2)
		)
		errs <- te1
		errs <- te2
		close(errs)
		_, err := parameters.AccumulateArgs(ctx, nil, errs)
		if err == nil {
			t.Error("expected an error but did not receive one - " +
				"errors were supplied in the input channel")
		}

		// XXX: This string is only for human readability in the event we get several errors.
		// Don't mimic this pattern in non-test code.
		for i, e := range []error{te1, te2} {
			var passed bool
			// Only the first error encountered will be wrapped.
			if i == 0 {
				passed = errors.Is(err, e)
			} else {
				passed = strings.Contains(err.Error(), e.Error())
			}
			if !passed {
				t.Errorf("input error not found in output error"+
					"\n\tlooking for: %v"+
					"\n\thave: %v",
					e, err)
			}
		}
	})

	t.Run("bad special values", func(t *testing.T) {
		// Magic: We only allow this to error (instead of panic)
		// for types that have special handlers/parsers.
		// Like time.Duration, multiaddr, etc.
		// Otherwise this will panic, as it should.
		var (
			settings   = new(testSettings)
			request, _ = cmds.NewRequest(ctx, nil,
				cmds.OptMap{
					embeddedParam1().CommandLine(): "not a maddr",
					embeddedParam2().CommandLine(): []string{"not a maddr"},
					embeddedParam3().CommandLine(): "not a duration",
				},
				nil, nil, &cmds.Command{})
			unsetArgs, errs = parameters.ParseSettings(ctx, settings,
				parameters.SettingsFromCmds(request),
				parameters.SettingsFromEnvironment(),
			)
			_, err = parameters.AccumulateArgs(ctx, unsetArgs, errs)
		)
		if err == nil {
			t.Error("expected error but got none (non-parseable special values)")
		}
	})

	t.Run("conflicting value typed", func(t *testing.T) {
		// Magic: We only allow this to error (instead of panic)
		// for types that have special handlers/parsers.
		// Like time.Duration, multiaddr, etc.
		// Otherwise this will panic, as it should.
		var (
			settings   = new(testSettings)
			badValue   = true
			request, _ = cmds.NewRequest(ctx, nil,
				cmds.OptMap{
					embeddedParam1().CommandLine(): badValue,
					embeddedParam2().CommandLine(): badValue,
					embeddedParam3().CommandLine(): badValue,
				},
				nil, nil, &cmds.Command{})
			unsetArgs, errs = parameters.ParseSettings(ctx, settings,
				parameters.SettingsFromCmds(request),
				parameters.SettingsFromEnvironment(),
			)
			_, err = parameters.AccumulateArgs(ctx, unsetArgs, errs)
		)
		if err == nil {
			t.Error("expected error but got none (value types mismatch)")
		}
	})
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

type testOptionEmbedded struct {
	NotEmbedded string `settings:"arguments"`
	testEmbed          // This should expand its fields into options to match its parameters.
	unrelated   bool
}

func (*testOptionEmbedded) Parameters() parameters.Parameters {
	return append(
		[]parameters.Parameter{simpleParam()},
		(*testEmbed)(nil).Parameters()...,
	)
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
		{
			"embedded",
			new(testOptionEmbedded),
			len((*testOptionEmbedded)(nil).Parameters()),
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
				t.Errorf("settings options do not match expected count"+
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
