package command_test

import (
	"context"
	"errors"
	"flag"
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

func noop(ctx context.Context, settings *tSettings, args ...string) error {
	return nil
}

func cMakeCommand(t *testing.T) {
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
		hlprGotWant(t, "Didn't fail when called with unexpected args", err, want)
	}
}

func hfNeedsHelp(t *testing.T) {
	t.Parallel()
	hf := new(command.HelpFlag)
	const want = false
	hlprHelpRequested(t, hf, want)
}

func hfSet(t *testing.T) {
	t.Parallel()
	hf := new(command.HelpFlag)
	const want = true
	if err := hf.Set("true"); err != nil {
		t.Fatal(err)
	}
	hlprHelpRequested(t, hf, want)
}

func hfString(t *testing.T) {
	t.Parallel()
	hf := new(command.HelpFlag)
	const want = "false"
	if got := hf.String(); got != want {
		hlprGotWant(t, "String returned unexpected value:", got, want)
	}
}

func hlprHelpRequested(t *testing.T, hf *command.HelpFlag, want bool) {
	t.Helper()
	if got := hf.HelpRequested(); got != want {
		hlprGotWant(t, "HelpFlag returned unexpected value:", got, want)
	}
}

func hlprGotWant(t *testing.T, explain string, got, want any) {
	t.Helper()
	t.Errorf(explain+
		"\n\tgot: %v"+
		"\n\twant: %v",
		got, want,
	)
}
