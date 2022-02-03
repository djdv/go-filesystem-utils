package parameters_test

import (
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

func TestParemeters(t *testing.T) {
	t.Parallel()
	t.Run("valid", testParametersValid)
	t.Run("cmds options", testOptions)
	t.Run("arguments", testArguments)
	t.Run("environment", testEnvironment)
}

func testParametersValid(t *testing.T) {
	t.Parallel()
	t.Run("names", testParameterNames)
}

func testParameterNames(t *testing.T) {
	t.Parallel()
	t.Run("aliases", testAliases)
}

func testAliases(t *testing.T) {
	t.Parallel()
	const paramName = "alias test"
	var (
		aliases = []string{
			"an alias",
			"a",
			"another alias",
		}
		aliasCount = len(aliases)
		aliasOpts  = func() []parameters.ParameterOption {
			aliasOpts := make([]parameters.ParameterOption, aliasCount)
			for i := range aliases {
				aliasOpts[i] = parameters.WithAlias(aliases[i])
			}
			return aliasOpts
		}()
		param = parameters.NewParameter("test param for .Name methods",
			append(
				aliasOpts,
				parameters.WithName(paramName),
				parameters.WithEnvironmentPrefix(paramName),
			)...,
		)
	)
	for _, source := range []parameters.SourceID{
		parameters.CommandLine,
		parameters.Environment,
	} {
		var (
			gotAliases = param.Aliases(source)
			gotCount   = len(gotAliases)
		)
		if gotCount != aliasCount {
			t.Errorf("aliases do not match"+
				"\n\tgot: %v"+
				"\n\twant: %v",
				gotAliases, aliases,
			)
		}
	}
}

func testParemeterInvalidArgs(t *testing.T) {
	t.Parallel()
	const failMsg = "invalid source ID value"
	var (
		invalidSource parameters.SourceID
		parameter     = (*testRootSettings)(nil).Parameters()[0]
	)

	t.Run("name", func(t *testing.T) {
		t.Parallel()
		testPanic(t, func() { parameter.Name(invalidSource) }, failMsg)
	})
	t.Run("aliases", func(t *testing.T) {
		t.Parallel()
		testPanic(t, func() { parameter.Aliases(invalidSource) }, failMsg)
	})
}

// Various invalid declarations/combinations.
type (
	// TODO: our tag prefix with the wrong or missing value
	testNotAStruct         bool
	testBadSettingsTagless struct {
		TestField  bool
		TestField2 bool
	}
	testBadSettingsMissingSettingsTag struct {
		TestField  bool `parameters:"notSettings"`
		TestField2 bool
	}
	testBadSettingsNonStandardTag struct {
		TestField  bool `parameters:"""settings"""`
		TestField2 bool
	}
	testBadSettingsShort struct {
		TestField bool `parameters:"settings"`
	}
	testBadSettingsUnassignable struct {
		testField  bool `parameters:"settings"`
		testField2 bool
	}
	// TODO: Check this. We can just test using another struct, prior to registering.
	testBadSettingsUnhandledType1 struct {
		TestField  interface{} `parameters:"settings"`
		TestField2 *interface{}
	}
	// TODO: Check this. We can accommodate these now
	testBadSettingsUnhandledType2 struct {
		TestField  complex128 `parameters:"settings"`
		TestField2 complex64
	}
)

func invalidParamSet() []parameters.Parameter {
	return parameters.Parameters{
		parameters.NewParameter("",
			parameters.WithName("bad param 0"),
		),
		parameters.NewParameter("",
			parameters.WithName("bad param 1"),
		),
	}
}

func (*testBadSettingsTagless) Parameters() parameters.Parameters { return invalidParamSet() }
func (*testBadSettingsMissingSettingsTag) Parameters() parameters.Parameters {
	return invalidParamSet()
}
func (*testBadSettingsNonStandardTag) Parameters() parameters.Parameters { return invalidParamSet() }
func (*testBadSettingsShort) Parameters() parameters.Parameters          { return invalidParamSet() }
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
		"wrong value",
		new(testBadSettingsTagless),
		"struct has different tag value than expected",
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
