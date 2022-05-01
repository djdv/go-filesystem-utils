package runtime_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

func TestRuntime(t *testing.T) {
	t.Parallel()
	t.Run("valid", testRuntimeValid)
	t.Run("invalid", testRuntimeInvalid)
}

type (
	settings struct {
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
	embeddedSettings struct {
		Extra int
		settings
	}
)

func (*settings) Parameters(ctx context.Context) parameters.Parameters {
	var (
		fieldCount  = reflect.TypeOf((*settings)(nil)).Elem().NumField()
		emptyParams = make([]runtime.CmdsParameter, fieldCount)
	)
	return runtime.MustMakeParameters[*settings](ctx, emptyParams)
}

func (*embeddedSettings) Parameters(ctx context.Context) parameters.Parameters {
	const embeddedStructs = 1
	var (
		fieldCount = reflect.TypeOf((*embeddedSettings)(nil)).Elem().NumField() -
			embeddedStructs
		emptyParams = make([]runtime.CmdsParameter, fieldCount)
	)
	return generic.CtxJoin(ctx,
		runtime.MustMakeParameters[*embeddedSettings](ctx, emptyParams),
		(*settings).Parameters(nil, ctx),
	)
}

func testRuntimeValid(t *testing.T) {
	t.Parallel()
	t.Run("cmds", testCmds)
	t.Run("reflect", testReflect)
	t.Run("assign", testAssign)
	t.Run("assign vector", testAssignVector)
	// t.Run("parse string", testParseString) // TODO: [lint] testing formerly exported func
	t.Run("parse struct", testParse)
}

func testCmds(t *testing.T) {
	t.Parallel()
	const (
		description       = "Tests the parameter formatter methods"
		expectedCLIName   = "param-name"
		expectedEnvPrefix = "TEST_PREFIX_"
		expectedENVName   = expectedEnvPrefix + "TPKG_PARAM_NAME"
	)
	var (
		param = runtime.CmdsParameter{
			Namespace:     "tpkg",
			OptionName:    "param name",
			HelpText:      description,
			EnvPrefix:     "test prefix",
			OptionAliases: []string{"t", "x"},
		}
		missmatch = func(which string, got, want interface{}) string {
			return fmt.Sprintf("parameter %s doesn't match"+
				"\n\tgot: %#v"+
				"\n\twant: %#v",
				which, got, want)
		}
	)
	if got := param.Description(); got != description {
		t.Errorf(missmatch("description", got, description))
	}
	for _, test := range []struct {
		source      parameters.SourceID
		wantName    string
		wantAliases []string
	}{
		{
			parameters.CommandLine,
			expectedCLIName,
			[]string{"t", "x"},
		},
		{
			parameters.Environment,
			expectedENVName,
			[]string{
				expectedEnvPrefix + "TPKG_T",
				expectedEnvPrefix + "TPKG_X",
			},
		},
	} {
		var (
			source      = test.source
			wantName    = test.wantName
			wantAliases = test.wantAliases
		)
		if got := param.Name(source); got != wantName {
			t.Errorf(missmatch(source.String(), got, wantName))
		}
		if got := param.Aliases(source); !reflect.DeepEqual(got, wantAliases) {
			t.Errorf(missmatch(source.String(), got, wantAliases))
		}
	}
}

func testReflect(t *testing.T) {
	t.Parallel()
	var (
		ctx         = context.Background()
		fieldCount  = reflect.TypeOf((*settings)(nil)).Elem().NumField()
		fieldMap    = make(map[string]struct{}, fieldCount)
		fields, err = runtime.ReflectFields[*settings](ctx)
	)
	if err != nil {
		t.Fatal(err)
	}
	for field := range fields {
		name := field.Name
		if _, ok := fieldMap[name]; ok {
			t.Error("duplicate field returned:", name)
		}
		fieldMap[name] = struct{}{}
	}
	if gotFields := len(fieldMap); gotFields != fieldCount {
		t.Errorf("fields received does not match field count"+
			"\n\tgot: %#v"+
			"\n\twant: %#v",
			gotFields, fieldCount)
	}
}

func testAssign(t *testing.T) {
	t.Parallel()
	t.Run("builtin", testAssignBuiltin)
	// t.Run("convert", testAssignConvert)
	t.Run("user parser", testAssignParser)
}

func testAssignBuiltin(t *testing.T) {
	t.Parallel()
	var (
		goValue       int
		expectedValue = 1
		arg           = runtime.Argument{
			ValueReference: &goValue,
		}
	)

	if err := runtime.ParseAndAssign(arg, expectedValue); err != nil {
		t.Fatal(err)
	}
	if goValue != expectedValue {
		t.Errorf("settings field values do not match input values"+
			"\n\tgot: %#v"+
			"\n\twant: %#v",
			goValue, expectedValue)
	}
}

/*
func testAssignConvert(t *testing.T) {
	t.Parallel()
	const expectedLiteral = 1
	var (
		goValue       int32
		expectedValue int64 = expectedLiteral
		arg                 = runtime.Argument{
			ValueReference: &goValue,
		}
	)

	if err := runtime.ParseAndAssign(arg, expectedValue); err != nil {
		t.Fatal(err)
	}
	if goValue != expectedLiteral {
		t.Errorf("settings field values do not match input values"+
			"\n\tgot: %#v"+
			"\n\twant: %#v",
			goValue, expectedValue)
	}
}
*/

func testAssignParser(t *testing.T) {
	t.Parallel()
	const stringValue = "1s"
	expectedValue, err := time.ParseDuration(stringValue)
	if err != nil {
		t.Fatal(err)
	}
	var (
		goValue time.Duration
		parser  = runtime.TypeParser{
			Type: reflect.TypeOf((*time.Duration)(nil)).Elem(),
			ParseFunc: func(argument string) (interface{}, error) {
				return time.ParseDuration(argument)
			},
		}
		arg = runtime.Argument{
			ValueReference: &goValue,
		}
	)

	if err := runtime.ParseAndAssign(arg, stringValue, parser); err != nil {
		t.Fatal(err)
	}
	if goValue != expectedValue {
		t.Errorf("assigned value does not match expected value"+
			"\n\tgot: %#v"+
			"\n\twant: %#v",
			goValue, expectedValue)
	}
}

// TODO: convert to ParseArg
/*
func testParseString(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input         string
		expectedValue interface{}
	}{
		{
			"string",
			"string",
		},
		{
			"true",
			true,
		},
		{
			"-1",
			int(-1),
		},
		{
			"1",
			uint(1),
		},
		{
			".5",
			float64(0.5),
		},
		{
			"-1i",
			complex128(-1i),
		},
		{
			"1,2,3",
			[]string{"1", "2", "3"},
		},
	} {
		var (
			input        = test.input
			expected     = test.expectedValue
			expectedType = reflect.TypeOf(expected)
			name         = expectedType.Name()
		)
		if name == "" { // Non-primitives like slices.
			name = expectedType.Kind().String()
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := runtime.ParseString(expectedType, input)
			if err != nil {
				t.Fatal(err)
			}
			if got != expected {
				t.Errorf("parsed value does not match expected value"+
					"\n\tgot: %#v"+
					"\n\twant: %#v",
					got, expected)
			}
		})
	}
}
*/

type parseFuncShim func(context.Context, []runtime.SetFunc) (interface{}, error)

func genericParseShim[setIntf runtime.SettingsConstraint[set], set any]() parseFuncShim {
	return func(ctx context.Context, sf []runtime.SetFunc) (interface{}, error) {
		return runtime.Parse[setIntf](ctx, sf)
	}
}

func testParse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, test := range []struct {
		name      string
		settings  parameters.Settings
		parseFunc parseFuncShim
	}{
		{
			"flat",
			nonzeroValues[settings](),
			genericParseShim[*settings](),
		},
		{
			"embedded",
			nonzeroValues[embeddedSettings](),
			genericParseShim[*embeddedSettings](),
		},
	} {
		var (
			name         = test.name
			wantSettings = test.settings
			parseFunc    = test.parseFunc
		)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var (
				sources       = []runtime.SetFunc{mockSettingsSource(wantSettings)}
				settings, err = parseFunc(ctx, sources)
			)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(wantSettings, settings) {
				t.Errorf("settings field values do not match input values"+
					"\n\tgot: %#v"+
					"\n\twant: %#v",
					settings, wantSettings)
			}
		})
	}
}

/*
	TODO lint

func testAssignVector(t *testing.T) {

	t.Parallel()
	for _, test := range []struct {
		name          string
		expectedValue interface{}
		input         interface{}
		parsers       []runtime.TypeParser
	}{
		{
			"builtin",
			[]int{1, 2},
			"1,2",
			nil,
		},
		{
			"parser",
			[]time.Duration{time.Duration(1), time.Duration(2)},
			"1ns,2ns",
			[]runtime.TypeParser{
				{
					Type: reflect.TypeOf((*time.Duration)(nil)).Elem(),
					ParseFunc: func(argument string) (interface{}, error) {
						return time.ParseDuration(argument)
					},
				},
			},
		},
	} {
		var (
			name          = test.name
			expectedValue = test.expectedValue
			goValue       = reflect.Zero(reflect.TypeOf(expectedValue)).Interface()
			arg           = runtime.Argument{
				ValueReference: &goValue,
			}
			input   = test.input
			parsers = test.parsers
		)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			t.Logf("goVal 1:%#v", goValue)
			if err := runtime.AssignToArgument(arg, input, parsers...); err != nil {
				t.Fatal(err)
			}
			t.Logf("goVal 2:%#v", goValue)
			if goValue != expectedValue {
				t.Errorf("settings field values do not match input values"+
					"\n\tgot: %#v"+
					"\n\twant: %#v",
					goValue, expectedValue)
			}
		})
	}

}
*/
func testAssignVector(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name          string
		expectedValue []int
		input         string
		parsers       []runtime.TypeParser
	}{
		{
			"builtin",
			[]int{1, 2},
			"1,2",
			nil,
		},
	} {
		var (
			name     = test.name
			expected = test.expectedValue
			input    = test.input
			parsers  = test.parsers
		)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var (
				value []int // TODO: dynamic type from test table / new zero value for arbitrary types
				arg   = runtime.Argument{
					ValueReference: &value,
				}
			)
			if err := runtime.ParseAndAssign(arg, input, parsers...); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(value, expected) {
				t.Errorf("settings field values do not match input values"+
					"\n\tgot: %#v"+
					"\n\twant: %#v",
					value, expected)
			}
		})
	}
}

func testRuntimeInvalid(t *testing.T) {
	t.Parallel()
	t.Run("cmds", testCmdsInvalid)
}

func testCmdsInvalid(t *testing.T) {
	t.Parallel()
	testPanic := func(t *testing.T, fn func(), failMsg string) {
		t.Helper()
		defer func(t *testing.T) {
			t.Helper()
			if r := recover(); r == nil {
				t.Errorf("expected to panic due to \"%s\" but did not", failMsg)
			} else {
				t.Log("recovered from (expected) panic:", r)
			}
		}(t)
		fn()
	}

	t.Run("SourceID", func(t *testing.T) {
		t.Parallel()
		var (
			source parameters.SourceID
			param  runtime.CmdsParameter
		)
		const failMsg = "invalid SourceID"
		testPanic(t, func() { param.Name(source) }, failMsg)
		testPanic(t, func() { param.Aliases(source) }, failMsg)
	})
}

func nonzeroValues[set any]() *set {
	var (
		settings = new(set)
		setValue = reflect.ValueOf(settings).Elem()
		typ      = setValue.Type()
	)
	for _, field := range reflect.VisibleFields(typ) {
		fieldRef := setValue.FieldByIndex(field.Index)
		switch field.Type.Kind() {
		case reflect.String:
			goValue := "a"
			fieldRef.Set(reflect.ValueOf(goValue))
		case reflect.Bool:
			goValue := true
			fieldRef.Set(reflect.ValueOf(goValue))
		case reflect.Int32: // NOTE: Rune alias
			goValue := int32('A')
			fieldRef.Set(reflect.ValueOf(goValue))
		case reflect.Int,
			reflect.Int8,
			reflect.Int16,
			reflect.Int64,
			reflect.Uint,
			reflect.Uint8,
			reflect.Uint16,
			reflect.Uint32,
			reflect.Uint64:
			var (
				goValue      = 1
				reflectValue = reflect.ValueOf(goValue).Convert(field.Type)
			)
			fieldRef.Set(reflectValue)
		case reflect.Float32,
			reflect.Float64:
			var (
				goValue      = 1.
				reflectValue = reflect.ValueOf(goValue).Convert(field.Type)
			)
			fieldRef.Set(reflectValue)

		case reflect.Complex64,
			reflect.Complex128:
			var (
				goValue      = 1 + 0i
				reflectValue = reflect.ValueOf(goValue).Convert(field.Type)
			)
			fieldRef.Set(reflectValue)
		}
	}
	return settings
}

func mockSettingsSource[set any](existing set) runtime.SetFunc {
	return func(ctx context.Context, argsToSet runtime.Arguments,
		parsers ...runtime.TypeParser,
	) (runtime.Arguments, <-chan error) {
		var (
			structValue = reflect.ValueOf(existing).Elem()
			fields      = reflect.VisibleFields(structValue.Type())
			unsetArgs   = make(chan runtime.Argument, cap(argsToSet))
			errs        = make(chan error)
		)
		go func() {
			defer close(unsetArgs)
			defer close(errs)
			var fieldIndex int
			fn := func(unsetArg runtime.Argument) (arg runtime.Argument, err error) {
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("panicked: %s", r)
					}
				}()
			skip:
				field := fields[fieldIndex]
				fieldIndex++
				if field.Anonymous {
					goto skip // HACK: Not expanding fields "properly" just for the test.
				}
				fieldValue := structValue.FieldByIndex(field.Index)
				runtime.ParseAndAssign(unsetArg, fieldValue.Interface())
				return unsetArg, generic.ErrSkip // We set the value, don't relay it.
			}

			generic.ProcessResults(ctx, argsToSet, unsetArgs, errs, fn)
		}()
		return unsetArgs, errs
	}
}
