package options_test

import (
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/options"
)

func ExampleConstructorOption() {
	options, err := options.MakeOptions[*emptySettings](options.WithBuiltin(true))
	if err != nil {
		// handle(err)
	}
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
