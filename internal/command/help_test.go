package command_test

import (
	"fmt"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/command"
)

func TestHelp(t *testing.T) {
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
			settings := command.Help(want)
			if got := settings.HelpRequested(); got != want {
				t.Errorf("helpflag mismatch"+
					"\n\tgot: %t"+
					"\n\twant: %t",
					got, want,
				)
			}
		})
	}
}
