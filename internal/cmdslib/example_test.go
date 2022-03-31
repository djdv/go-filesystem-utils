package cmdslib_test

import (
	"context"
	"fmt"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

// Each part of this full example
// is explained in more detail
// in the examples on each type.

// Below we define a few abstract `Parameters`
// that `Settings` interfaces may return as a list.

func exampleParam() parameters.Parameter {
	return parameters.NewParameter(
		"Example parameter's description text.",
	)
}

func exampleParam2() parameters.Parameter {
	return parameters.NewParameter(
		"I'm used to demonstrate the parameter library.",
		parameters.WithRootNamespace(),             // Omit some extra identifiers
		parameters.WithName("Something Different"), // CLI: --something-different
		parameters.WithEnvironmentPrefix("t"),      // ENV: $T_SOMETHING_DIFFERENT
	)
}

type ExampleSettings struct {
	TheStart bool `parameters:"settings"`
	TheEnd   bool
}

func (*ExampleSettings) Parameters() parameters.Parameters {
	return parameters.Parameters{
		exampleParam(),
		exampleParam2(),
	}
}

func Example() {
	// We can use the Parameter interface
	// to figure out what key-names to use when storing values.
	var (
		firstParam = exampleParam()
		envKey     = firstParam.Name(parameters.Environment)
	)
	os.Setenv(envKey, "true")
	defer os.Unsetenv(envKey)

	// The library will pick up values using the same names internally,
	// and assign them to the struct during `Parse`.
	var (
		settings = new(ExampleSettings)
		// Value sources are provided in precedence order.
		// If a field cannot be set by one source,
		// it is relayed to the next `parameters.Source` provided to `Parse`.
		sources = []parameters.SetFunc{
			parameters.SettingsFromEnvironment(),
		}
		err = parameters.Parse(context.Background(), settings, sources)
	)
	_ = err // Handle the error in real code!

	fmt.Println(settings.TheStart)
	// Output: true
}
