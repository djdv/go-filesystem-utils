package cmdslib_test

import (
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

func ExampleSettings_basic() {
	// This struct may be used in its own pkg,
	// but may also be imported and/or embedded by others.
	// As if it was declared in another pkg as `root.ExampleSettings`.
	type ExampleSet struct {
		// Unrelated fields may share the same struct,
		// preceding and/or following the desired settings fields.
		notASetting struct{}
		// All settings fields must be settable by the library.
		// (exported by Go / start with uppercase)
		//
		// To tell the library where to start looking for settings
		// declare the following struct tag on the desired field.
		TheStart bool `parameters:"settings"`
		// There is no end marker. Parsers use the parameter list
		// returned by the `Parameters` method of the `Settings` interface,
		// to determine when to stop parsing fields.
		// (If the struct contains less fields than parameters,
		// an error or panic will occur depending on the library function being called)
		TheEnd          bool
		alsoNotASetting struct{}
	}
	/*
		Give the `ExampleSettings` struct a list of parameters
		by defining the `Parameters` method:

		func (*ExampleSettings) Parameters() parameters.Parameters {
			return parameters.Parameters{
				exampleParam(),  // Associated with `TheStart`
				exampleParam2(), // Associated with `TheEnd`, etc.
			}
		}
	*/
}

// Parameters constructed via functions is the preferred method
// (since the library can infer a lot of information automatically)
//
// But inline construction is possible if the caller provides
// details that would otherwise be missing.
//
// Contrast the output names that are generated automatically
// with the ones generated via caller provided options.
func ExampleParameter_inline() {
	var (
		verboseParam = parameters.NewParameter(
			"A description of what this parameter is used for",
			parameters.WithNamespace("Some Namespace"),
			parameters.WithName("More Useful Parameter"),
			parameters.WithEnvironmentPrefix("Some Prefix"),
		)

		simpleParam = parameters.NewParameter(
			"Port to use for X server",
			parameters.WithName("another name"),
		)

		rootParam = parameters.NewParameter(
			"Example of special (omitted) namespace",
			parameters.WithRootNamespace(),
			parameters.WithName("yet another name"),
		)

		lessHelpfulParam = parameters.NewParameter("not very useful")
	)

	// Output:
	// simpleParam:
	// 	CLI: --another-name
	// 	ENV: $PARAMETERS_TEST_PARAMETERS_TEST_ANOTHER_NAME
	// verboseParam:
	// 	CLI: --more-useful-parameter
	// 	ENV: $SOME_PREFIX_SOME_NAMESPACE_MORE_USEFUL_PARAMETER
	// rootParam:
	// 	CLI: --yet-another-name
	// 	ENV: $PARAMETERS_TEST_YET_ANOTHER_NAME
	// lessHelpfulParam:
	// 	CLI: --example-parameter-_-inline
	// 	ENV: $PARAMETERS_TEST_PARAMETERS_TEST_EXAMPLE_PARAMETER___INLINE

	for _, pair := range []struct {
		param     parameters.Parameter
		fmtHeader string
	}{
		{
			simpleParam,
			"simpleParam",
		},
		{
			verboseParam,
			"verboseParam",
		},

		{
			rootParam,
			"rootParam",
		},
		{
			lessHelpfulParam,
			"lessHelpfulParam",
		},
	} {
		printParamExampleText(pair.fmtHeader, pair.param)
	}
}
