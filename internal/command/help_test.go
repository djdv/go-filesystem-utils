package command_test

import (
	"fmt"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/command"
)

func TestHelp(t *testing.T) {
	t.Parallel()
	t.Run("HelpFlag", HelpArg)
}

// HelpArg tests if `-help` text has the correct output
func HelpArg(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		want bool
	}{
		{true},
		{false},
	} {
		want := test.want
		t.Run(fmt.Sprint(want), func(t *testing.T) {
			t.Parallel()
			helpArg := command.HelpArg(want)
			if got := helpArg.Help(); got != want {
				t.Errorf("helpflag mismatch"+
					"\n\tgot: %t"+
					"\n\twant: %t",
					got, want,
				)
			}
		})
	}
}
