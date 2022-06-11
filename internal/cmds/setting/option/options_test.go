package option_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/option"
	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/runtime"
	"github.com/djdv/go-filesystem-utils/internal/parameter"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// TODO: [all] remove `test` prefix. It's implied and adds nothing when not required.
func TestOptions(t *testing.T) {
	t.Parallel()
	t.Run("valid", optionsValid)
	t.Run("invalid", optionsInvalid)
}

func optionsValid(t *testing.T) {
	t.Parallel()
	t.Run("constructor", optionsConstructor)
	t.Run("cmdslib", optionsCmds)
	t.Run("go", optionsReflect)
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

func (sf fieldParam) Name(parameter.Provider) string      { return sf.StructField.Name }
func (sf fieldParam) Description() string                 { return sf.Type.String() }
func (sf fieldParam) Aliases(parameter.Provider) []string { return nil }

func generateParams[setPtr runtime.SettingsType[settings],
	settings any](ctx context.Context,
) parameter.Parameters {
	var (
		fields = reflect.VisibleFields(reflect.TypeOf((setPtr)(nil)).Elem())
		params = make(chan parameter.Parameter, len(fields))
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

func (*emptySettings) Parameters(ctx context.Context) parameter.Parameters {
	return generateParams[*emptySettings](ctx)
}

func (*builtinSettings) Parameters(ctx context.Context) parameter.Parameters {
	return generateParams[*builtinSettings](ctx)
}

func (*vectorSettings) Parameters(ctx context.Context) parameter.Parameters {
	return generateParams[*vectorSettings](ctx)
}

func (*CompoundSettings) Parameters(ctx context.Context) parameter.Parameters {
	return generateParams[*CompoundSettings](ctx)
}

type embeddedOrder = bool

const (
	embeddedInHead embeddedOrder = true
	embeddedInTail               = false
)

func embeddedSettingsParams[setPtr runtime.SettingsType[settings],
	settings any](ctx context.Context, order embeddedOrder,
) parameter.Parameters {
	localField, hasField := reflect.TypeOf((setPtr)(nil)).Elem().FieldByName("Extra")
	if !hasField {
		panic("missing expected `Extra` test field")
	}

	var (
		embeddedParams = generateParams[setPtr](ctx)
		localParam     = fieldParam{localField}
		params         = make(chan parameter.Parameter, cap(embeddedParams)+1)
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

func (*embeddedSettingsHead) Parameters(ctx context.Context) parameter.Parameters {
	return embeddedSettingsParams[*embeddedSettingsHead](ctx, embeddedInHead)
}

func (*embeddedSettingsTail) Parameters(ctx context.Context) parameter.Parameters {
	return embeddedSettingsParams[*embeddedSettingsTail](ctx, embeddedInTail)
}

func optionsCmds(t *testing.T) {
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
		opts, err    = option.MakeOptions[*emptySettings](option.WithBuiltin(true))
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

func optionsReflect(t *testing.T) {
	t.Parallel()
	type optionConstructor func(...option.ConstructorOption) ([]cmds.Option, error)
	var (
		somethingNoopParser = func(string) (somethingDifferent, error) {
			return somethingDifferent{}, nil
		}
		somethingOptMaker = option.NewOptionConstructor(somethingNoopParser)
		constructorOpts   = []option.ConstructorOption{
			option.WithConstructor(somethingOptMaker),
		}
	)
	for _, test := range []struct {
		name string
		optionConstructor
		opts []option.ConstructorOption
	}{
		{
			"builtin",
			option.MakeOptions[*builtinSettings],
			nil,
		},
		{
			"vector",
			option.MakeOptions[*vectorSettings],
			nil,
		},
		{
			"custom",
			option.MakeOptions[*CompoundSettings],
			constructorOpts,
		},
		{
			"embedded before",
			option.MakeOptions[*embeddedSettingsHead],
			constructorOpts,
		},
		{
			"embedded after",
			option.MakeOptions[*embeddedSettingsTail],
			constructorOpts,
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

func optionsConstructor(t *testing.T) {
	t.Parallel()
	var (
		expectedDefault time.Duration = 30
		name                          = "test"
		aliases                       = []string{"t"}
		description                   = "a test option"
		optMaker                      = option.NewOptionConstructor(time.ParseDuration)
		baseOpt                       = optMaker.NewOption(name, description, aliases...)
		opt                           = baseOpt.WithDefault(expectedDefault)
	)
	const durationString = "15s"
	expectedParse, err := time.ParseDuration(durationString)
	if err != nil {
		t.Fatal(err)
	}

	const fmtString = "option returned unexpected %s:" +
		"\n\tgot: %v" +
		"\n\twant: %v"

	if got := opt.Name(); got != name {
		t.Errorf(fmtString, "name", got, name)
	}
	if got := opt.Description(); got != description {
		t.Errorf(fmtString, "description", got, description)
	}
	if gotAliases := opt.Names(); len(gotAliases) < 1 ||
		!reflect.DeepEqual(gotAliases[1:], aliases) {
		t.Errorf(fmtString, "aliases", gotAliases, aliases)
	}
	if gotDefault := opt.Default(); !reflect.DeepEqual(gotDefault, expectedDefault) {
		t.Errorf(fmtString, "default value", gotDefault, expectedDefault)
	}

	expectedKind := reflect.TypeOf(expectedDefault).Kind()
	if gotKind := opt.Type(); gotKind != expectedKind {
		t.Errorf(fmtString, "value type", gotKind, expectedKind)
	}

	gotParsed, err := opt.Parse(durationString)
	if err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual(gotParsed, expectedParse) {
		t.Errorf(fmtString, "parsed value", gotParsed, expectedParse)
	}
}

func optionsConstructorInvalid(t *testing.T) {
	t.Parallel()
	defer func(t *testing.T) {
		t.Helper()
		if r := recover(); r == nil {
			t.Errorf("expected to panic due to mismatched value types, but did not")
		} else {
			t.Log("recovered from (expected) panic:", r)
		}
	}(t)
	optMaker := option.NewOptionConstructor(func(string) (struct{}, error) {
		return struct{}{}, nil
	})
	optMaker.NewOption("invalid", "should panic").WithDefault(nil)
}

type (
	settingsUnassignable struct {
		testField  bool
		testField2 bool
	}
	settingsUnhandledSettingsType bool
	settingsUnhandledFieldType    struct {
		TestField any
	}
)

func (*settingsUnassignable) Parameters(ctx context.Context) parameter.Parameters {
	return generateParams[*settingsUnassignable](ctx)
}

func (*settingsUnhandledSettingsType) Parameters(ctx context.Context) parameter.Parameters {
	return nil
}

func (*settingsUnhandledFieldType) Parameters(ctx context.Context) parameter.Parameters {
	return generateParams[*settingsUnhandledFieldType](ctx)
}

func optionsInvalid(t *testing.T) {
	t.Parallel()
	t.Run("constructor", optionsConstructorInvalid)
	t.Run("go", optionsReflectInvalid)
}

func optionsReflectInvalid(t *testing.T) {
	t.Parallel()
	type optionConstructor func(...option.ConstructorOption) ([]cmds.Option, error)
	for _, test := range []struct {
		expectedErr error
		optionConstructor
		name string
	}{
		{
			runtime.ErrUnassignable,
			option.MakeOptions[*settingsUnassignable],
			"unassignable fields",
		},
		{
			runtime.ErrUnexpectedType,
			option.MakeOptions[*settingsUnhandledSettingsType],
			"uses unhandled settings type",
		},
		{
			runtime.ErrUnexpectedType,
			option.MakeOptions[*settingsUnhandledFieldType],
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
