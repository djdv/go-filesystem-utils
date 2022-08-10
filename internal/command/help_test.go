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
			var (
				helpArg    = new(command.HelpArg)
				stringWant = fmt.Sprint(want)
			)
			if err := helpArg.Set(stringWant); err != nil {
				t.Fatal(err)
			}
			if got := helpArg.Help(); got != want {
				t.Errorf("helpflag mismatch"+
					"\n\tgot: %t"+
					"\n\twant: %t",
					got, want,
				)
			}
			if got := helpArg.String(); got != stringWant {
				t.Errorf("helpflag format mismatch"+
					"\n\tgot: %s"+
					"\n\twant: %s",
					got, stringWant,
				)
			}
		})
	}
}
