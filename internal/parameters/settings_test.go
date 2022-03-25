package parameters_test

import (
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	"github.com/multiformats/go-multiaddr"
)

type (
	testRootSettings struct {
		TestField  bool `parameters:"settings"`
		TestField2 int
	}

	testPkgSettings struct {
		testRootSettings
		Struct           testStructType
		Simple           int16
		MultiLevelStruct testEmbeddedStructType
	}

	testSubPkgSettings struct {
		testPkgSettings
		testFlatSettings
		testVectorSettings
		C, D int
	}

	testFlatSettings struct {
		Bool       bool `parameters:"settings"`
		Complex64  complex64
		Complex128 complex128
		Float32    float32
		Float64    float64
		Int        int
		Int8       int8
		Int16      int16
		Int32      int32
		Int64      int64
		Rune       rune
		String     string
		Uint       uint
		Uint8      uint8
		Uint16     uint16
		Uint32     uint32
		Uint64     uint64
	}

	testVectorSettings struct {
		Slice []bool `parameters:"settings"`
		Array [8]bool
	}

	testStructType struct {
		A uint
		B int64
	}

	testEmbeddedStructType struct {
		testStructType
		C uint64
		D float64
		E string
		F multiaddr.Multiaddr
		G []rune
	}

	testVectorExternalHandlerSettings struct {
		Slice []multiaddr.Multiaddr `parameters:"settings"`
		Array [2]multiaddr.Multiaddr
	}

	TestExportedStructType struct {
		Z int // Intentionally untagged.
		Y int
	}

	testEmbeddedStructSettings struct {
		A int `parameters:"settings"`
		B int
		TestExportedStructType
	}

	testCompoundSettings struct {
		CompoundValue testStructType `parameters:"settings"`
		CompoundField int
	}
)

func (*testFlatSettings) Parameters() parameters.Parameters {
	return parameterMaker[testFlatSettings]()
}

func (*testVectorSettings) Parameters() parameters.Parameters {
	return parameterMaker[testVectorSettings]()
}

func (*testVectorExternalHandlerSettings) Parameters() parameters.Parameters {
	return parameterMaker[testVectorExternalHandlerSettings]()
}

func (*testCompoundSettings) Parameters() parameters.Parameters {
	return parameterMaker[testCompoundSettings]()
}

func (*testSubPkgSettings) Parameters() parameters.Parameters {
	return combineParameters(
		(*testPkgSettings)(nil).Parameters(),
		(*testFlatSettings)(nil).Parameters(),
		(*testVectorSettings)(nil).Parameters(),
		parameterMaker[testSubPkgSettings](),
	)
}

func (self *testRootSettings) Parameters() parameters.Parameters {
	return parameterMaker[testRootSettings]()
}

func (self *testPkgSettings) Parameters() parameters.Parameters {
	return combineParameters(
		(*testRootSettings)(nil).Parameters(),
		parameterMaker[testPkgSettings](),
	)
}

func (*testEmbeddedStructSettings) Parameters() parameters.Parameters {
	return combineParameters(
		parameterMaker[testEmbeddedStructSettings](),
		parameterMaker[TestExportedStructType](),
	)
}

// Various invalid declarations/combinations.
type (
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
	testBadSettingsUnhandledType struct {
		TestField  interface{} `parameters:"settings"`
		TestField2 *interface{}
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
func (*testBadSettingsUnhandledType) Parameters() parameters.Parameters  { return invalidParamSet() }
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
		new(testBadSettingsUnhandledType),
		"this Settings interface contains types we don't account for",
	},
	{
		"invalid concrete type",
		testNotAStruct(true),
		"this Settings interface is not a pointer",
	},
}
