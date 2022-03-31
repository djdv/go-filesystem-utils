package cmdslib_test

import (
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	"github.com/multiformats/go-multiaddr"
)

type (
	rootSettings struct {
		TestField  bool `parameters:"settings"`
		TestField2 int
	}

	pkgSettings struct {
		rootSettings
		Struct           structType
		Simple           int16
		MultiLevelStruct embeddedStructType
	}

	subPkgSettings struct {
		pkgSettings
		flatSettings
		vectorSettings
		C, D int
	}

	flatSettings struct {
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

	externalType = multiaddr.Multiaddr

	vectorSettings struct {
		Slice []bool `parameters:"settings"`
		Array [8]bool
	}
	vectorExternalHandlerSettings struct {
		Slice []externalType `parameters:"settings"`
		Array [2]externalType
	}

	externalSettings struct {
		ExternalType externalType `parameters:"settings"`
	}

	structType struct {
		A uint
		B int64
	}

	embeddedStructType struct {
		structType
		C uint64
		D float64
		E string
		F externalType
		G []rune
	}

	ExportedStructType struct {
		Z int // Intentionally untagged.
		Y int
	}

	embeddedStructSettings struct {
		A int `parameters:"settings"`
		B int
		ExportedStructType
	}

	compoundSettings struct {
		CompoundValue structType `parameters:"settings"`
		CompoundField int
	}
)

func (*flatSettings) Parameters() parameters.Parameters {
	return parameterMaker[flatSettings]()
}

func (*vectorSettings) Parameters() parameters.Parameters {
	return parameterMaker[vectorSettings]()
}

func (*vectorExternalHandlerSettings) Parameters() parameters.Parameters {
	return parameterMaker[vectorExternalHandlerSettings]()
}

func (*compoundSettings) Parameters() parameters.Parameters {
	return parameterMaker[compoundSettings]()
}

func (*externalSettings) Parameters() parameters.Parameters {
	return parameterMaker[externalSettings]()
}

func (*subPkgSettings) Parameters() parameters.Parameters {
	return combineParameters(
		(*pkgSettings)(nil).Parameters(),
		(*flatSettings)(nil).Parameters(),
		(*vectorSettings)(nil).Parameters(),
		parameterMaker[subPkgSettings](),
	)
}

func (self *rootSettings) Parameters() parameters.Parameters {
	return parameterMaker[rootSettings]()
}

func (self *pkgSettings) Parameters() parameters.Parameters {
	return combineParameters(
		(*rootSettings)(nil).Parameters(),
		parameterMaker[pkgSettings](),
	)
}

func (*embeddedStructSettings) Parameters() parameters.Parameters {
	return combineParameters(
		parameterMaker[embeddedStructSettings](),
		parameterMaker[ExportedStructType](),
	)
}

// Various invalid declarations/combinations.
type (
	notAStruct      bool
	settingsTagless struct {
		TestField  bool
		TestField2 bool
	}
	settingsMissingSettingsTag struct {
		TestField  bool `parameters:"notSettings"`
		TestField2 bool
	}
	settingsNonStandardTag struct {
		TestField  bool `parameters:"""settings"""`
		TestField2 bool
	}
	settingsShort struct {
		TestField bool `parameters:"settings"`
	}
	settingsUnassignable struct {
		testField  bool `parameters:"settings"`
		testField2 bool
	}
	settingsUnhandledType struct {
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

func (*settingsTagless) Parameters() parameters.Parameters { return invalidParamSet() }
func (*settingsMissingSettingsTag) Parameters() parameters.Parameters {
	return invalidParamSet()
}
func (*settingsNonStandardTag) Parameters() parameters.Parameters { return invalidParamSet() }
func (*settingsShort) Parameters() parameters.Parameters          { return invalidParamSet() }
func (*settingsUnassignable) Parameters() parameters.Parameters   { return invalidParamSet() }
func (*settingsUnhandledType) Parameters() parameters.Parameters  { return invalidParamSet() }
func (notAStruct) Parameters() parameters.Parameters              { return invalidParamSet() }

type invalidInterfaceSet struct {
	name            string
	settingsIntf    parameters.Settings
	nonErrorMessage string
}

var invalidInterfaces = []invalidInterfaceSet{
	{
		"tagless",
		new(settingsTagless),
		"struct has no tag",
	},
	{
		"wrong value",
		new(settingsTagless),
		"struct has different tag value than expected",
	},
	{
		"malformed tag",
		new(settingsNonStandardTag),
		"struct has non-standard tag",
	},
	{
		"fewer fields",
		new(settingsShort),
		"struct has fewer fields than parameters",
	},
	{
		"unassignable fields",
		new(settingsUnassignable),
		"struct fields are not assignable by reflection",
	},
	{
		"invalid concrete type",
		new(notAStruct),
		"this Settings interface is not a struct",
	},
	{
		"uses unhandled types",
		new(settingsUnhandledType),
		"this Settings interface contains types we don't account for",
	},
	{
		"invalid concrete type",
		notAStruct(true),
		"this Settings interface is not a pointer",
	},
}
