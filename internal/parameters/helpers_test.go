package parameters_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

func testPanic(t *testing.T, fn func(), failMsg string) {
	t.Helper()
	defer func(t *testing.T) {
		t.Helper()
		if r := recover(); r == nil {
			t.Errorf("expected to panic due to \"%s\" but did not", failMsg)
		} else {
			t.Log("recovered from (expected) panic:\n\t", r)
		}
	}(t)
	fn()
}

func requestAndEnvSources(request *cmds.Request) []parameters.SetFunc {
	return []parameters.SetFunc{
		parameters.SettingsFromCmds(request),
		parameters.SettingsFromEnvironment(),
	}
}

func typeParsers() []parameters.TypeParser {
	var (
		maddrType   = reflect.TypeOf((*multiaddr.Multiaddr)(nil)).Elem()
		maddrParser = func(argument string) (interface{}, error) {
			return multiaddr.NewMultiaddr(argument)
		}
		durationType   = reflect.TypeOf((*time.Duration)(nil)).Elem()
		durationParser = func(argument string) (interface{}, error) {
			return time.ParseDuration(argument)
		}
	)
	return []parameters.TypeParser{
		{
			Type:      maddrType,
			ParseFunc: maddrParser,
		},
		{
			Type:      durationType,
			ParseFunc: durationParser,
		},
	}
}

func optionMakers() []parameters.CmdsOptionOption {
	var (
		maddrType    = reflect.TypeOf((*multiaddr.Multiaddr)(nil)).Elem()
		durationType = reflect.TypeOf((*time.Duration)(nil)).Elem()
		makers       = []parameters.OptionMaker{
			{
				Type:           maddrType,
				MakeOptionFunc: cmds.StringOption,
			},
			{
				Type:           durationType,
				MakeOptionFunc: cmds.StringOption,
			},
		}
		opts = make([]parameters.CmdsOptionOption, len(makers))
	)
	for i, maker := range makers {
		opts[i] = parameters.WithMaker(maker)
	}
	return opts
}

// parameterMaker generates dynamic parameters for the passed in Settings struct.
// It only creates parameters for exported, non-embedded fields.
func parameterMaker(set parameters.Settings) parameters.Parameters {
	var (
		typ        = reflect.TypeOf(set).Elem()
		fieldCount = typ.NumField()
		params     = make([]parameters.Parameter, 0, fieldCount)
	)
	for _, field := range reflect.VisibleFields(typ) {
		if !field.IsExported() {
			continue
		}
		if len(field.Index) > 1 {
			continue
		}
		var (
			name        = field.Name
			description = fmt.Sprintf(
				"Dynamic parameter for %s",
				name,
			)
		)
		params = append(params,
			parameters.NewParameter(
				description,
				parameters.WithName(name),
			),
		)
	}
	return params
}

func optionFromSettings(set parameters.Settings) cmds.OptMap {
	var (
		currentParam int
		setVal       = reflect.ValueOf(set).Elem()
		params       = set.Parameters()
		paramCount   = len(params)
		options      = make(cmds.OptMap, paramCount)
	)
	for _, field := range reflect.VisibleFields(setVal.Type()) {
		if field.Anonymous {
			continue
		}
		name := params[currentParam].Name(parameters.CommandLine)
		options[name] = setVal.FieldByIndex(field.Index).Interface()
		currentParam++
		if currentParam == paramCount {
			break
		}
	}
	return options
}

func nonzeroOptionSetter(set parameters.Settings) cmds.OptMap {
	var (
		typ     = reflect.TypeOf(set).Elem()
		params  = set.Parameters()
		options = make(cmds.OptMap, len(params))
	)
	for i, field := range reflect.VisibleFields(typ) {
		name := params[i].Name(parameters.CommandLine)
		switch field.Type.Kind() {
		case reflect.String:
			options[name] = "a"
		case reflect.Bool:
			options[name] = "true"
		case reflect.Int32: // NOTE: Rune alias
			options[name] = "65" // ASCII 'A'
		case reflect.Int,
			reflect.Int8,
			reflect.Int16,
			reflect.Int64,
			reflect.Uint,
			reflect.Uint8,
			reflect.Uint16,
			reflect.Uint32,
			reflect.Uint64,
			reflect.Float32,
			reflect.Float64,
			reflect.Complex64,
			reflect.Complex128:
			options[name] = "1"
		}
	}
	return options
}

func nonzeroValueSetter(set parameters.Settings) {
	var (
		setValue = reflect.ValueOf(set).Elem()
		typ      = setValue.Type()
	)
	for i, field := range reflect.VisibleFields(typ) {
		fieldRef := setValue.FieldByIndex([]int{i})
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
}

func testCompareSettingsStructs(t *testing.T, gotSettings, wantSettings parameters.Settings) {
	t.Helper()
	if !reflect.DeepEqual(gotSettings, wantSettings) {
		t.Fatalf("settings field values do not match input values"+
			"\n\tgot:"+
			"\n\t%#v"+ // These long structs get their own lines.
			"\n\twant:"+
			"\n\t%#v",
			gotSettings, wantSettings,
		)
	}
}

func combineParameters(sets ...parameters.Parameters) parameters.Parameters {
	var count int
	for _, set := range sets {
		count += len(set)
	}

	var (
		current int
		all     = make([]parameters.Parameter, count)
	)
	for _, set := range sets {
		current += copy(all[current:], set)
	}
	return all
}
