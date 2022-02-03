package parameters_test

import (
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	"github.com/multiformats/go-multiaddr"
)

type (
	unrelatedEmbed struct {
		pointless bool
		and       byte
		unused    int
	}

	testRootSettings struct {
		unrelatedField1 int8
		unrelatedField2 int16
		unrelatedField3 struct{}
		unrelatedEmbed
		TestField  bool `parameters:"settings"`
		TestField2 int
	}

	testPkgSettings struct {
		unrelatedField int32
		testRootSettings
		Struct         testStructType
		Simple         int16
		EmbeddedStruct testEmbeddedStructType
		testSubPkgSettings
		unrelatedField2 int64
	}

	testSubPkgSettings struct {
		testFlatSettings
		testVectorSettings
		testCompoundSettings
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

	testVectorExternalSettings struct {
		Slice []multiaddr.Multiaddr `parameters:"settings"`
		Array [2]multiaddr.Multiaddr
	}

	TestExportedStructType struct {
		Z int
		Y int
	}

	testEmbeddedStructSettings struct {
		A int `parameters:"settings"`
		B int
		TestExportedStructType
	}

	compoundValue        struct{ A, B int }
	testCompoundSettings struct {
		CompoundValue compoundValue `parameters:"settings"`
	}
)

func (self *testFlatSettings) Parameters() parameters.Parameters   { return parameterMaker(self) }
func (self *testVectorSettings) Parameters() parameters.Parameters { return parameterMaker(self) }
func (self *testVectorExternalSettings) Parameters() parameters.Parameters {
	return parameterMaker(self)
}
func (self *testCompoundSettings) Parameters() parameters.Parameters { return parameterMaker(self) }
func (self *testSubPkgSettings) Parameters() parameters.Parameters {
	return combineParameters(
		(*testFlatSettings)(nil).Parameters(),
		(*testVectorSettings)(nil).Parameters(),
		(*testCompoundSettings)(nil).Parameters(),
		parameterMaker(self),
	)
}

func (self *testRootSettings) Parameters() parameters.Parameters {
	return parameterMaker(self)
}
func (self *testPkgSettings) Parameters() parameters.Parameters {
	return combineParameters(
		(*testRootSettings)(nil).Parameters(),
		parameterMaker(self),
		(*testSubPkgSettings)(nil).Parameters(),
	)
}

func (self *testEmbeddedStructSettings) Parameters() parameters.Parameters {
	return parameterMaker(self)
}
