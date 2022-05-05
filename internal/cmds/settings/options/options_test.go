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

	fieldParam struct{ reflect.StructField }
)

func (sf fieldParam) Name(parameters.SourceID) string      { return sf.StructField.Name }
func (sf fieldParam) Description() string                  { return sf.Type.String() }
func (sf fieldParam) Aliases(parameters.SourceID) []string { return nil }

func (opts cmdsOptions) String() string {
	optNames := make([]string, len(opts))
	for i, opt := range opts {
		optNames[i] = opt.Name()
	}
	return strings.Join(optNames, ", ")
}

func generateParams[setPtr runtime.SettingsConstraint[set], set any](ctx context.Context) parameters.Parameters {
	var (
		fields = reflect.VisibleFields(reflect.TypeOf((setPtr)(nil)).Elem())
		params = make(chan parameters.Parameter, len(fields))
	)
	go func() {
		defer close(params)
		for _, field := range fields {
			if ctx.Err() != nil {
				return
			}
			params <- fieldParam{field}
		}
	}()
	return params
}

func (*empty) Parameters(ctx context.Context) parameters.Parameters {
	return generateParams[*empty](ctx)
}

func (*builtinSettings) Parameters(ctx context.Context) parameters.Parameters {
	return generateParams[*builtinSettings](ctx)
}

func (*vectorSettings) Parameters(ctx context.Context) parameters.Parameters {
	return generateParams[*vectorSettings](ctx)
}

func (*compoundSettings) Parameters(ctx context.Context) parameters.Parameters {
	return generateParams[*compoundSettings](ctx)
}

func testOptionsCmds(t *testing.T) {
	t.Parallel()
	var (
		builtinNames = []string{
			cmds.OptionEncodingType.Name(),
			cmds.OptionTimeout.Name(),
			cmds.OptionStreamChannels.Name(),
			cmds.OptLongHelp,
			cmds.OptShortHelp,
		}
		builtinCount = len(builtinNames)
		opts         = options.MustMakeCmdsOptions[*empty](options.WithBuiltin(true))
	)
	if opts == nil {
		t.Error("nil options returned")
	}
	if gotCount := len(opts); gotCount != builtinCount {
		optNames := func() []string {
			names := make([]string, gotCount)
			for i, opt := range opts {
				names[i] = opt.Name()
			}
			return names
		}()
		t.Errorf("builtin options count does not match expected count"+
			"\n\tgot: [%d]%v"+
			"\n\twant: [%d]%v",
			gotCount, optNames, builtinCount, builtinNames)
	}
}

func testOptionsReflect(t *testing.T) {
	t.Parallel()
	type optionConstructor func(...options.ConstructorOption) []cmds.Option
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
			[]options.ConstructorOption{options.WithMaker(
				options.OptionMaker{
					Type:           reflect.TypeOf((*somethingDifferent)(nil)).Elem(),
					MakeOptionFunc: cmds.StringOption,
				},
			)},
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
	settingsUnassignable struct {
		testField  bool
		testField2 bool
	}
	settingsUnhandledType struct {
		TestField interface{}
	}
)

func (*settingsUnassignable) Parameters(ctx context.Context) parameters.Parameters {
	return generateParams[*settingsUnassignable](ctx)
}

func (*settingsUnhandledType) Parameters(ctx context.Context) parameters.Parameters {
	return generateParams[*settingsUnhandledType](ctx)
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
			"unassignable fields",
			options.MustMakeCmdsOptions[*settingsUnassignable],
			"struct fields are not assignable by reflection",
		},
		{
			"uses unhandled types",
			options.MustMakeCmdsOptions[*settingsUnhandledType],
			"this Settings interface contains types we don't account for",
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
