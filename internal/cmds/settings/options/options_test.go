package options_test

import (
	"context"
	"errors"
	"reflect"
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
	t.Run("cmdslib", testOptionsCmds)
	t.Run("go", testOptionsReflect)
}

type (
	emptySettings   struct{}
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
	CompoundSettings   struct {
		NonPrim somethingDifferent
	}

	// TODO: we should have a counterpart invalid test which is these but with an unexported
	// `compountSettings` - this will get refused by the lib since it's unassignable.
	embeddedSettingsHead struct {
		CompoundSettings
		Extra bool
	}
	embeddedSettingsTail struct {
		Extra bool
		CompoundSettings
	}
	fieldParam struct{ reflect.StructField }
)

func (sf fieldParam) Name(parameters.SourceID) string      { return sf.StructField.Name }
func (sf fieldParam) Description() string                  { return sf.Type.String() }
func (sf fieldParam) Aliases(parameters.SourceID) []string { return nil }

func generateParams[setPtr runtime.SettingsType[settings],
	settings any](ctx context.Context,
) parameters.Parameters {
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

func (*emptySettings) Parameters(ctx context.Context) parameters.Parameters {
	return generateParams[*emptySettings](ctx)
}

func (*builtinSettings) Parameters(ctx context.Context) parameters.Parameters {
	return generateParams[*builtinSettings](ctx)
}

func (*vectorSettings) Parameters(ctx context.Context) parameters.Parameters {
	return generateParams[*vectorSettings](ctx)
}

func (*CompoundSettings) Parameters(ctx context.Context) parameters.Parameters {
	return generateParams[*CompoundSettings](ctx)
}

type embeddedOrder = bool

const (
	embeddedInHead embeddedOrder = true
	embeddedInTail               = false
)

func embeddedSettingsParams[setPtr runtime.SettingsType[settings],
	settings any](ctx context.Context, order embeddedOrder,
) parameters.Parameters {
	localField, hasField := reflect.TypeOf((setPtr)(nil)).Elem().FieldByName("Extra")
	if !hasField {
		panic("missing expected `Extra` test field")
	}

	var (
		embeddedParams = generateParams[setPtr](ctx)
		localParam     = fieldParam{localField}
		params         = make(chan parameters.Parameter, cap(embeddedParams)+1)
	)
	defer close(params)
	if order == embeddedInTail {
		params <- localParam
	}
	for param := range embeddedParams {
		params <- param
	}
	if order == embeddedInHead {
		params <- localParam
	}
	return params
}

func (*embeddedSettingsHead) Parameters(ctx context.Context) parameters.Parameters {
	return embeddedSettingsParams[*embeddedSettingsHead](ctx, embeddedInHead)
}

func (*embeddedSettingsTail) Parameters(ctx context.Context) parameters.Parameters {
	return embeddedSettingsParams[*embeddedSettingsTail](ctx, embeddedInTail)
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
		opts, err    = options.MakeOptions[*emptySettings](options.WithBuiltin(true))
	)
	if err != nil {
		t.Fatal(err)
	}
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
	type optionConstructor func(...options.ConstructorOption) ([]cmds.Option, error)
	typeHandlerOpts := []options.ConstructorOption{options.WithMaker(
		options.TypeConstructor{
			Type:          reflect.TypeOf((*somethingDifferent)(nil)).Elem(),
			NewOptionFunc: cmds.StringOption,
		},
	)}
	for _, test := range []struct {
		name string
		optionConstructor
		opts []options.ConstructorOption
	}{
		{
			"builtin",
			options.MakeOptions[*builtinSettings],
			nil,
		},
		{
			"vector",
			options.MakeOptions[*vectorSettings],
			nil,
		},
		{
			"custom",
			options.MakeOptions[*CompoundSettings],
			typeHandlerOpts,
		},
		{
			"embedded before",
			options.MakeOptions[*embeddedSettingsHead],
			typeHandlerOpts,
		},
		{
			"embedded after",
			options.MakeOptions[*embeddedSettingsTail],
			typeHandlerOpts,
		},
	} {
		var (
			name            = test.name
			constructor     = test.optionConstructor
			constructorOpts = test.opts
		)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			opts, err := constructor(constructorOpts...)
			if err != nil {
				t.Fatal(err)
			}
			if opts == nil {
				t.Error("nil options returned")
			}
		})
	}
}

type (
	settingsUnassignable struct {
		testField  bool
		testField2 bool
	}
	settingsUnhandledSettingsType bool
	settingsUnhandledFieldType    struct {
		TestField interface{}
	}
)

func (*settingsUnassignable) Parameters(ctx context.Context) parameters.Parameters {
	return generateParams[*settingsUnassignable](ctx)
}

func (*settingsUnhandledSettingsType) Parameters(ctx context.Context) parameters.Parameters {
	return nil
}

func (*settingsUnhandledFieldType) Parameters(ctx context.Context) parameters.Parameters {
	return generateParams[*settingsUnhandledFieldType](ctx)
}

func testOptionsInvalid(t *testing.T) {
	t.Parallel()
	t.Run("go", testOptionsReflectInvalid)
}

func testOptionsReflectInvalid(t *testing.T) {
	t.Parallel()
	type optionConstructor func(...options.ConstructorOption) ([]cmds.Option, error)
	for _, test := range []struct {
		expectedErr error
		optionConstructor
		name string
	}{
		{
			runtime.ErrUnassignable,
			options.MakeOptions[*settingsUnassignable],
			"unassignable fields",
		},
		{
			runtime.ErrUnexpectedType,
			options.MakeOptions[*settingsUnhandledSettingsType],
			"uses unhandled settings type",
		},
		{
			runtime.ErrUnexpectedType,
			options.MakeOptions[*settingsUnhandledFieldType],
			"uses unhandled field types",
		},
	} {
		var (
			expectedErr = test.expectedErr
			constructor = test.optionConstructor
			testName    = test.name
		)
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			if _, err := constructor(); !errors.Is(err, expectedErr) {
				t.Errorf("constructor returned unexpected error:"+
					"\n\tgot: %s"+
					"\n\twant: %s",
					err, expectedErr,
				)
			}
		})
	}
}
