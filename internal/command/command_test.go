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
	t.Run("HelpFlag", HelpFlag)
	t.Run("Command", Command)
}

func HelpFlag(t *testing.T) {
	t.Parallel()
	t.Run("HelpArg", HelpArg)
}

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
			if got := helpArg.HelpRequested(); got != want {
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

func Command(t *testing.T) {
	t.Parallel()
	t.Run("MakeCommand", cmdMake)
	t.Run("Execute", cmdExecute)
}

func cmdMake(t *testing.T) {
	cmd := command.MakeCommand[*settings](
		noopName, noopSynopsis, noopUsage, noop,
		command.WithSubcommands(testCommands[noopName]),
	)
	if usage := cmd.Usage(); usage == "" {
		t.Errorf("usage string for command \"%s\", is empty", noopName)
	}
}

func cmdExecute(t *testing.T) {
	t.Parallel()
	t.Run("valid", exeValid)
	t.Run("invalid", exeInvalid)
}

type settings struct {
	command.HelpArg
	someField bool
}

func (ts *settings) BindFlags(fs *flag.FlagSet) {
	ts.HelpArg.BindFlags(fs)
	fs.BoolVar(&ts.someField, "sf", false, "Some Flag")
}

const (
	synopisSuffix = " Synopis"
	usageSuffix   = " Usage"

	noopName     = "noop"
	noopSynopsis = noopName + synopisSuffix
	noopUsage    = noopName + usageSuffix

	noopArgsName     = "noopArgs"
	noopArgsSynopsis = noopArgsName + synopisSuffix
	noopArgsUsage    = noopArgsName + usageSuffix
)

type cmdMap map[string]command.Command

var testCommands = cmdMap{
	noopName: command.MakeCommand[*settings](
		noopName, noopSynopsis, noopUsage, noop,
	),
	noopArgsName: command.MakeCommand[*settings](
		noopArgsName, noopArgsSynopsis, noopArgsUsage, noopArgs,
	),
}

func copyCmds(cmds cmdMap) cmdMap {
	clone := make(cmdMap, len(cmds))
	for key, cmd := range testCommands {
		clone[key] = cmd
	}
	return clone
}

func noop(ctx context.Context, set *settings) error {
	return nil
}

func noopArgs(ctx context.Context, set *settings, args ...string) error {
	return nil
}

func exeValid(t *testing.T) {
	t.Parallel()
	const (
		subsName     = "subcommands"
		subsSynopsis = subsName + synopisSuffix
		subsUsage    = subsName + usageSuffix

		subName     = "subcommand"
		subSynopsis = subName + synopisSuffix
		subUsage    = subName + usageSuffix
	)

	cmds := copyCmds(testCommands)
	cmds[subsName] = command.MakeCommand[*settings](
		subsName, subsSynopsis, subsUsage, noopArgs,
		command.WithSubcommands(
			command.MakeCommand[*settings](
				subName, subSynopsis, subUsage, noop,
			),
		),
	)
	for _, test := range []struct {
		name string
		args []string
	}{
		{
			noopName,
			nil,
		},
		{
			noopArgsName,
			[]string{"arg1", "arg2"},
		},
		{
			subsName,
			[]string{subName},
		},
	} {
		var (
			name = test.name
			args = test.args
		)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := cmds[name].Execute(ctx, args...); err != nil {
				t.Error(err)
			}
		})
	}
}

func exeInvalid(t *testing.T) {
	t.Parallel()
	var (
		discard = io.Discard.(command.StringWriter)
		cmds    = cmdMap{
			noopName: command.MakeCommand[*settings](
				noopName, noopSynopsis, noopUsage, noop,
				command.WithUsageOutput(discard),
			),
			noopArgsName: command.MakeCommand[*settings](
				noopArgsName, noopArgsSynopsis, noopArgsUsage, noopArgs,
				command.WithUsageOutput(discard),
			),
		}
	)
	for _, test := range []struct {
		name     string
		args     []string
		expected error
		reason   string
	}{
		{
			noopName,
			[]string{"arg1", "arg2"},
			command.ErrUsage,
			"niladic function called with args",
		},
		{
			noopName,
			[]string{"-help"},
			command.ErrUsage,
			"function called with help flag",
		},
	} {
		var (
			name     = test.name
			args     = test.args
			expected = test.expected
			reason   = test.reason
		)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := cmds[name].Execute(ctx, args...)
			if !errors.Is(err, expected) {
				t.Errorf("did not receive expected error"+
					"\n\tgot: %s"+
					"\n\twant: %s"+
					"\n\twhy: %s",
					err, expected, reason,
				)
			}
		})
	}
}
