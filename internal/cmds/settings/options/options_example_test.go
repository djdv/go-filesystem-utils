package options_test

import (
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/options"
)

func ExampleConstructorOption() {
	options := options.MustMakeCmdsOptions[*emptySettings](options.WithBuiltin(true))
	for _, option := range options {
		fmt.Println(option.Name())
	}
	// Output:
	// encoding
	// timeout
	// stream-channels
	// help
	// h
}
