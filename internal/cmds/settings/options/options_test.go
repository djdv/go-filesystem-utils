package options_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/options"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

func TestOptions(t *testing.T) {
	t.Parallel()
	t.Run("valid", testOptionsValid)
	t.Run("invalid", testOptionsInvalid)
}

func testOptionsValid(t *testing.T) {
	t.Parallel()
	t.Run("cmds-lib", testOptionsCmds)
	t.Run("go", testOptionsReflect)
}

type (
	cmdsOptions     []cmds.Option
	empty           struct{}
	builtinSettings struct {
		Bool       bool
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

	vectorSettings struct {
		Bools []bool
	}

	somethingDifferent struct{}
	compoundSettings   struct {
		NonPrim somethingDifferent
	}
)

func (opts cmdsOptions) String() string {
	optNames := make([]string, len(opts))
	for i, opt := range opts {
		optNames[i] = opt.Name()
	}
	return strings.Join(optNames, ", ")
}

func emptyParams[setPtr runtime.SettingsConstraint[set], set any](ctx context.Context) parameters.Parameters {
	var (
		fieldCount  = reflect.TypeOf((*set)(nil)).Elem().NumField()
		emptyParams = make([]runtime.CmdsParameter, fieldCount)
	)
	return runtime.MustMakeParameters[setPtr](ctx, emptyParams)
}

func (*empty) Parameters(ctx context.Context) parameters.Parameters {
	return runtime.MustMakeParameters[*empty](ctx, nil)
}

func (*builtinSettings) Parameters(ctx context.Context) parameters.Parameters {
	return emptyParams[*builtinSettings](ctx)
}

func (*vectorSettings) Parameters(ctx context.Context) parameters.Parameters {
	return emptyParams[*vectorSettings](ctx)
}

func (*compoundSettings) Parameters(ctx context.Context) parameters.Parameters {
	return emptyParams[*compoundSettings](ctx)
}

func testOptionsCmds(t *testing.T) {
	t.Parallel()
	opts := options.MustMakeCmdsOptions[*empty](options.WithBuiltin(true))
	if opts == nil {
		t.Error("nil options returned")
	}
	t.Log("got options:", cmdsOptions(opts))
}

func testOptionsReflect(t *testing.T) {
	t.Parallel()
	type optionConstructor func(...options.ConstructorOption) []cmds.Option
	customParser := options.OptionMaker{
		Type:           reflect.TypeOf((*somethingDifferent)(nil)).Elem(),
		MakeOptionFunc: cmds.StringOption,
	}
	_ = customParser
	for _, test := range []struct {
		name string
		optionConstructor
		opts []options.ConstructorOption
	}{
		{
			"builtin",
			options.MustMakeCmdsOptions[*builtinSettings],
			nil,
		},
		{
			"vector",
			options.MustMakeCmdsOptions[*vectorSettings],
			nil,
		},
		{
			"custom",
			options.MustMakeCmdsOptions[*compoundSettings],
			[]options.ConstructorOption{options.WithMaker(customParser)},
		},
	} {
		var (
			name            = test.name
			constructor     = test.optionConstructor
			constructorOpts = test.opts
		)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			opts := constructor(constructorOpts...)
			if opts == nil {
				t.Error("nil options returned")
			}
			t.Log("got options:", cmdsOptions(opts))
		})
	}
}

type (
	notAStruct    bool
	settingsShort struct {
		TestField bool
	}
	settingsLong struct {
		TestField  bool
		TestField2 bool
		TestField3 bool
	}
	settingsUnassignable struct {
		testField  bool
		testField2 bool
	}
	settingsUnhandledType struct {
		TestField interface{}
	}
)

func (notAStruct) Parameters(ctx context.Context) parameters.Parameters {
	return emptyParams[*notAStruct](ctx)
}
func (*settingsShort) Parameters(ctx context.Context) parameters.Parameters {
	params := []runtime.CmdsParameter{
		{OptionName: "field 1 should be defined"},
		{OptionName: "field 2 should not be defined"},
	}
	return runtime.MustMakeParameters[*settingsShort](ctx, params)
}
func (*settingsUnassignable) Parameters(ctx context.Context) parameters.Parameters {
	return emptyParams[*settingsUnassignable](ctx)
}
func (*settingsUnhandledType) Parameters(ctx context.Context) parameters.Parameters {
	return emptyParams[*settingsUnhandledType](ctx)
}

func testOptionsInvalid(t *testing.T) {
	t.Parallel()
	t.Run("go", testOptionsReflectInvalid)
}

func testOptionsReflectInvalid(t *testing.T) {
	t.Parallel()
	type optionConstructor func(...options.ConstructorOption) []cmds.Option
	testPanic := func(t *testing.T, constructor optionConstructor, failMsg string) {
		t.Helper()
		defer func(t *testing.T) {
			t.Helper()
			if r := recover(); r == nil {
				t.Errorf("expected to panic due to \"%s\" but did not", failMsg)
			} else {
				t.Log("recovered from (expected) panic:", r)
			}
		}(t)
		constructor()
	}

	for _, test := range []struct {
		name string
		optionConstructor
		nonErrorMessage string
	}{
		{
			"fewer fields",
			options.MustMakeCmdsOptions[*settingsShort],
			"struct has fewer fields than parameters",
		},
		{
			"unassignable fields",
			options.MustMakeCmdsOptions[*settingsUnassignable],
			"struct fields are not assignable by reflection",
		},
		{
			"invalid concrete type",
			options.MustMakeCmdsOptions[*notAStruct],
			"this Settings interface is not a struct",
		},
		{
			"uses unhandled types",
			options.MustMakeCmdsOptions[*settingsUnhandledType],
			"this Settings interface contains types we don't account for",
		},
		{
			"invalid concrete type",
			options.MustMakeCmdsOptions[*notAStruct],
			"this Settings interface is not a pointer",
		},
	} {
		var (
			testName    = test.name
			constructor = test.optionConstructor
			failMsg     = test.nonErrorMessage
		)
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			testPanic(t, constructor, failMsg)
		})
	}
}

/*

func testOptionsEmbedded(t *testing.T) {
	t.Parallel()
	var (
		settings      = new(embeddedStructSettings)
		expectedCount = len((*embeddedStructSettings)(nil).Parameters())
	)
	opts := parameters.MustMakeCmdsOptions(settings)
	if optLen := len(opts); expectedCount != optLen {
		optStrings := make([]string, optLen)
		for i, opt := range opts {
			optStrings[i] = opt.Name()
		}
		t.Errorf("settings options do not match expected count"+
			"\n\tgot: {%d}[%s]"+
			"\n\twant: {%d}[...]",
			optLen, strings.Join(optStrings, ", "),
			expectedCount,
		)
	}
}

func testOptionsOptions(t *testing.T) {
	t.Parallel()
	var (
		settings   = new(rootSettings)
		paramCount = len(settings.Parameters())
		opts       = parameters.MustMakeCmdsOptions(settings,
			parameters.WithBuiltin(true),
		)
	)
	if len(opts) <= paramCount {
		t.Error("built-in cmds options requested but options list returned is <= parameter count")
	}
}

func testLiteralsValid(t *testing.T) {
	t.Parallel()
	genOpts := func(name string) []parameters.ParameterOption {
		return []parameters.ParameterOption{
			parameters.WithName(name),
			parameters.WithNamespace("testspace"),
		}
	}
	for _, test := range []struct {
		fn   func(name string)
		name string
	}{
		{
			func(name string) {
				func() {
					parameters.NewParameter("lambda test", genOpts(name)...)
				}()
			},
			"lambda (with options)",
		},
	} {
		name := test.name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			test.fn(name) // If we don't panic, we're good.
		})
	}
}
*/
