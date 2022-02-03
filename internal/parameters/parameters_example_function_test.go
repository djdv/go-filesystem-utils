package parameters_test

import (
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

// Parameters constructed via functions can
// infer a lot of information automatically
// from the Go runtime.
//
// For convenience, parameter names are derived from the name of the function that returns them.
// Names may be specified via options passed to `NewParameter` to override this behaviour,
// but it is not required. Only a description of the paramater's purpose is required.
//
// It's the easiest way to construct and re-use parameters with this library.
func ExampleParameter_functional() {
	var (
		regularExample = RegularConstructor()
		customExample  = ComplexConstructor("custom prefix")
	)

	// Note that the binary name and package name
	// for Go tests are often the same.
	// In actual code the Environment name would look like
	// BINARY_NAME_PACKAGE_NAME_PARAM_NAME automatically.

	// Output:
	// plain constructor:
	// 	CLI: --regular-constructor
	// 	ENV: $PARAMETERS_TEST_PARAMETERS_TEST_REGULAR_CONSTRUCTOR
	// custom constructor:
	// 	CLI: --complex-constructor
	// 	ENV: $CUSTOM_PREFIX_PARAMETERS_TEST_COMPLEX_CONSTRUCTOR

	for _, pair := range []struct {
		param     parameters.Parameter
		fmtHeader string
	}{
		{
			regularExample,
			"plain constructor",
		},
		{
			customExample,
			"custom constructor",
		},
	} {
		printParamExampleText(pair.fmtHeader, pair.param)
	}
}

// These constructors and/or their returned values,
// can be called wherever parameters are needed;
// such as the `Parameters` method of the `Settings` interface,
// or anywhere common names are desired for setting or getting.
// e.g.
// - A command line argument constructor (`exec.Command`)
// - The process environment (`os.SetEnv`,`os.GetEnv`)

func RegularConstructor() parameters.Parameter {
	return parameters.NewParameter("example's description")
}

func ComplexConstructor(prefix string) parameters.Parameter {
	return parameters.NewParameter(
		"example's description",
		parameters.WithEnvironmentPrefix(prefix),
	)
}

func printParamExampleText(header string, param parameters.Parameter) {
	fmt.Printf(
		"%s:\n"+
			"\tCLI: --%s\n"+
			"\tENV: $%s\n",
		header,
		param.Name(parameters.CommandLine),
		param.Name(parameters.Environment),
	)
}
