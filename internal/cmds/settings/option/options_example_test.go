package option_test

import (
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/option"
)

func ExampleConstructorOption() {
	options, err := option.MakeOptions[*emptySettings](option.WithBuiltin(true))
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
