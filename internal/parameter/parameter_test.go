package parameter_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/parameter"
)

func ExampleParameter() {
	var (
		param    exampleParam
		cli      = param.Name(parameter.CommandLine)
		aliases  = param.Aliases(parameter.CommandLine)
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

func (exampleParam) Name(parameter.Provider) string { return "parameter-name" }
func (exampleParam) Description() string            { return "The port to use for the server." }
func (exampleParam) Aliases(parameter.Provider) []string {
	return []string{"p", "former-name", "name-parameter"}
}

func TestStringer(t *testing.T) {
	t.Parallel() // If this test doesn't compile `go generate` needs to be run.
	t.Run("valid", func(t *testing.T) { t.Parallel(); _ = parameter.CommandLine.String() })
	t.Run("invalid", func(t *testing.T) { t.Parallel(); _ = parameter.Provider(0).String() })
}
