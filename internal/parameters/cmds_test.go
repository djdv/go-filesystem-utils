package parameters_test

import (
	"strings"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

func testOptions(t *testing.T) {
	t.Parallel()
	t.Run("invalid", testOptionsInvalid)
	t.Run("valid", testOptionsValid)
}

func testOptionsInvalid(t *testing.T) {
	t.Parallel()
	for _, test := range invalidInterfaces {
		var (
			testName = test.name
			settings = test.settingsIntf
			failMsg  = test.nonErrorMessage
		)
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			testPanic(t, func() { parameters.MustMakeCmdsOptions(settings) }, failMsg)
		})
	}
}

func testOptionsValid(t *testing.T) {
	t.Parallel()
	t.Run("regular", testOptionsRegular)
	t.Run("embedded", testOptionsEmbedded)
	t.Run("constructor options", testOptionsOptions)
	t.Run("literals", testLiteralsValid)
}

func testOptionsRegular(t *testing.T) {
	t.Parallel()
	constructorOpts := optionMakers()
	for _, test := range []struct {
		settingsIntf parameters.Settings
		name         string
		count        int
	}{
		{
			new(testRootSettings),
			"root",
			len(parameterMaker[testRootSettings]()),
		},
		{
			new(testPkgSettings),
			"pkg",
			len(parameterMaker[testPkgSettings]()),
		},
		{
			new(testSubPkgSettings),
			"subpkg",
			len(parameterMaker[testSubPkgSettings]()),
		},
	} {
		var (
			testName      = test.name
			settings      = test.settingsIntf
			expectedCount = test.count
		)
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			opts := parameters.MustMakeCmdsOptions(settings, constructorOpts...)
			if optLen := len(opts); expectedCount != optLen {
				optStrings := make([]string, optLen)
				for i, opt := range opts {
					optStrings[i] = opt.Name()
				}
				t.Log("👀 test-dbg, params:", settings.Parameters())
				t.Errorf("settings options do not match expected count"+
					"\n\tgot: {%d}[%s]"+
					"\n\twant: {%d}[$filteredOpts]",
					optLen, strings.Join(optStrings, ", "),
					expectedCount,
				)
			}
		})
	}
}

func testOptionsEmbedded(t *testing.T) {
	t.Parallel()
	var (
		settings      = new(testEmbeddedStructSettings)
		expectedCount = len((*testEmbeddedStructSettings)(nil).Parameters())
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
		settings   = new(testRootSettings)
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
