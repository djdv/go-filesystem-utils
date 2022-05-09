package parameters_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

func ExampleParameter() {
	var (
		param    exampleParam
		cli      = param.Name(parameters.CommandLine)
		aliases  = param.Aliases(parameters.CommandLine)
		helpText = param.Description()
	)

	fmt.Println(cli)
	fmt.Println(strings.Join(aliases, ", "))
	fmt.Println(helpText)

	// Output:
	// parameter-name
	// p, former-name, name-parameter
	// The port to use for the server.
}

type exampleParam struct{}

func (exampleParam) Name(parameters.SourceID) string { return "parameter-name" }
func (exampleParam) Description() string             { return "The port to use for the server." }
func (exampleParam) Aliases(parameters.SourceID) []string {
	return []string{"p", "former-name", "name-parameter"}
}

func TestSringer(t *testing.T) {
	t.Parallel() // If this test doesn't compile `go generate` needs to be run.
	t.Run("valid", func(t *testing.T) { t.Parallel(); _ = parameters.CommandLine.String() })
	t.Run("invalid", func(t *testing.T) { t.Parallel(); _ = parameters.SourceID(0).String() })
}
