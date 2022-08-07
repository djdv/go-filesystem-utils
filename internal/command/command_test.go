package command_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/command"
)

func TestCommand(t *testing.T) {
	t.Parallel()
	t.Run("HelpFlag", tHelpFlag)
	t.Run("Command", tCommand)
}

func tHelpFlag(t *testing.T) {
	t.Parallel()
	t.Run("NeedsHelp", hfNeedsHelp)
	t.Run("Set", hfSet)
	t.Run("String", hfString)
}

func tCommand(t *testing.T) {
	t.Parallel()
	t.Run("MakeCommand", cMakeCommand)
}

type tSettings struct {
	command.HelpFlag
	someField bool
}

func (ts *tSettings) BindFlags(fs *flag.FlagSet) {
	command.NewHelpFlag(fs, &ts.HelpFlag)
	fs.BoolVar(&ts.someField, "sf", false, "Some Flag")
}

func cMakeCommand(t *testing.T) {
	noop := func(ctx context.Context, settings *tSettings, args ...string) error { return nil }
	t.Parallel()
	const (
		name    = "Name"
		synopis = "Synopis"
		usage   = "Usage"
	)
	var (
		want      = command.ErrUsage
		sw        = (io.Discard).(io.StringWriter)
		ctx       = context.Background() // TODO cancel ctx
		cmdNoOpts = command.MakeCommand(name, synopis, usage, noop)
		options   = []command.Option{
			command.WithoutArguments(true),
			command.WithUsageOutput(sw),
			command.WithSubcommands(cmdNoOpts),
		}
		cmdWithOpts = command.MakeCommand(name, synopis, usage, noop, options...)
	)

	if err := cmdNoOpts.Execute(ctx); err != nil {
		t.Error(err)
	}

	if err := cmdWithOpts.Execute(ctx); err != nil {
		t.Error(err)
	}

	if err := cmdWithOpts.Execute(ctx, "unexpected", "arguments"); !errors.Is(
		err, want) {
		hlprGotWant(t, err, want, "Didn't fail when called with unexpected args")
	}
}

func hfNeedsHelp(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		want bool
		flag command.HelpFlag
	}{
		{want: false, flag: command.HelpFlag(false)},
		{want: true, flag: command.HelpFlag(true)},
	} {
		var (
			want = test.want
			hf   = test.flag
		)
		t.Run(fmt.Sprint(want), func(t *testing.T) {
			t.Parallel()
			if got := hf.HelpRequested(); got != want {
				hlprGotWant(t, got, want, "HelpFlag returned unexpected value:")
			}
		})
	}
}

func hfSet(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		want bool
		flag command.HelpFlag
	}{
		{want: true, flag: command.HelpFlag(true)},
		{want: false, flag: command.HelpFlag(false)},
	} {
		var (
			want = test.want
			hf   = test.flag
		)
		t.Run(fmt.Sprint(want), func(t *testing.T) {
			t.Parallel()
			if err := hf.Set(fmt.Sprint(want)); err != nil {
				t.Fatal(err)
			}
			if got := hf.HelpRequested(); got != want {
				hlprGotWant(t, got, want, "HelpFlag returned unexpected value:")
			}
		})
	}
}

func hfString(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		want bool
		flag command.HelpFlag
	}{
		{want: true, flag: command.HelpFlag(true)},
		{want: false, flag: command.HelpFlag(false)},
	} {
		var (
			want = test.want
			hf   = test.flag
		)
		if got := hf.String(); got != fmt.Sprint(want) {
			hlprGotWant(t, got, want, "String returned unexpected value:")
		}
	}
}

func hlprGotWant(t *testing.T, got, want any, explain string) {
	t.Helper()
	t.Errorf(explain+
		"\n\tgot: %v"+
		"\n\twant: %v",
		got, want,
	)
}
