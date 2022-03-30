package reflect_test

import (
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

func TestParameters(t *testing.T) {
	t.Parallel()
	t.Run("valid", testParametersValid)
	t.Run("cmds options", testOptions)
	t.Run("arguments", testArguments)
	t.Run("environment", testEnvironment)
}

func testParametersValid(t *testing.T) {
	t.Parallel()
	t.Run("names", testParameterNames)
}

func testParameterNames(t *testing.T) {
	t.Parallel()
	t.Run("aliases", testAliases)
}

func testAliases(t *testing.T) {
	t.Parallel()
	const paramName = "alias test"
	var (
		aliases = []string{
			"an alias",
			"a",
			"another alias",
		}
		aliasCount = len(aliases)
		aliasOpts  = func() []parameters.ParameterOption {
			aliasOpts := make([]parameters.ParameterOption, aliasCount)
			for i := range aliases {
				aliasOpts[i] = parameters.WithAlias(aliases[i])
			}
			return aliasOpts
		}()
		param = parameters.NewParameter("test param for .Name methods",
			append(
				aliasOpts,
				parameters.WithName(paramName),
				parameters.WithEnvironmentPrefix(paramName),
			)...,
		)
	)
	for _, source := range []parameters.SourceID{
		parameters.CommandLine,
		parameters.Environment,
	} {
		var (
			gotAliases = param.Aliases(source)
			gotCount   = len(gotAliases)
		)
		if gotCount != aliasCount {
			t.Errorf("aliases do not match"+
				"\n\tgot: %v"+
				"\n\twant: %v",
				gotAliases, aliases,
			)
		}
	}
}

func testParemeterInvalidArgs(t *testing.T) {
	t.Parallel()
	const failMsg = "invalid source ID value"
	var (
		invalidSource parameters.SourceID
		parameter     = (*rootSettings)(nil).Parameters()[0]
	)
	t.Run("name", func(t *testing.T) {
		t.Parallel()
		testPanic(t, func() { parameter.Name(invalidSource) }, failMsg)
	})
	t.Run("aliases", func(t *testing.T) {
		t.Parallel()
		testPanic(t, func() { parameter.Aliases(invalidSource) }, failMsg)
	})
}
