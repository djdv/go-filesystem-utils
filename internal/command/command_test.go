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
	t.Run("Usage", cmdUsage)
	t.Run("Exec", cmdExecute)
}

type tSettings struct {
	command.HelpArg
	someField bool
}

func (ts *tSettings) BindFlags(fs *flag.FlagSet) {
	ts.HelpArg.BindFlags(fs)
	fs.BoolVar(&ts.someField, "sf", false, "Some Flag")
}

func noopArgs(ctx context.Context, settings *tSettings, args ...string) error {
	return nil
}

func noopNoArgs(ctx context.Context, settings *tSettings) error {
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
		wantErr      = command.ErrUsage
		stringWriter = (io.Discard).(command.StringWriter)
		ctx          = context.Background() // TODO cancel ctx
		cmdNoOpts    = command.MakeCommand[*tSettings](name, synopis, usage, noopArgs)

		options = []command.Option{
			command.WithUsageOutput(stringWriter),
			command.WithSubcommands(cmdNoOpts),
		}
		cmdWithOpts = command.MakeCommand[*tSettings](name, synopis, usage, noopNoArgs, options...)
	)

	if err := cmdNoOpts.Execute(ctx); err != nil {
		t.Error(err)
	}

	if err := cmdWithOpts.Execute(ctx); err != nil {
		t.Error(err)
	}

	if err := cmdWithOpts.Execute(ctx, "unexpected", "arguments"); !errors.Is(
		err, wantErr) {
		hlprGotWant(t, err, wantErr, "Didn't fail when called with unexpected args")
	}
}

func cmdUsage(t *testing.T) {
	t.Parallel()
	const (
		synopis = "Synopis"
		usage   = "Usage"
	)
	// TODO: output to buffer, translate test into an Example.
	// Compare via [Output:] comment.
	//
	// For now, if we don't panic, test will trace / cover.

	for _, test := range []struct {
		name    string
		exec    any
		options []command.Option
	}{
		{
			"nil",
			noopNoArgs,
			nil,
		},
		{
			"args",
			noopArgs,
			nil,
		},
		{
			"subs",
			noopArgs,
			[]command.Option{
				command.WithSubcommands(
					command.MakeCommand[*tSettings]("sub", synopis, usage, noopNoArgs),
				),
			},
		},
	} {
		var (
			name    = test.name
			exec    = test.exec
			options = test.options
		)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			switch execFn := exec.(type) {
			case func(context.Context, *tSettings, ...string) error:
				command.MakeCommand[*tSettings](name, synopis, usage, execFn, options...).Usage()
			case func(context.Context, *tSettings) error:
				command.MakeCommand[*tSettings](name, synopis, usage, execFn, options...).Usage()
			default:
				// TODO: real message
				t.Errorf("bad case: %#v", execFn)
			}
		})
	}
}

func cmdExecute(t *testing.T) {
	t.Skip("NIY")
}

func hfNeedsHelp(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		want bool
		flag command.HelpArg
	}{
		{
			want: false,
			flag: command.HelpArg(false),
		}, {
			want: true,
			flag: command.HelpArg(true),
		},
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
	}{
		{
			want: true,
		}, {
			want: false,
		},
	} {
		want := test.want
		t.Run(fmt.Sprint(want), func(t *testing.T) {
			t.Parallel()
			hf := new(command.HelpArg)
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
		flag command.HelpArg
	}{
		{
			want: true,
			flag: command.HelpArg(true),
		},
		{
			want: false,
			flag: command.HelpArg(false),
		},
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
