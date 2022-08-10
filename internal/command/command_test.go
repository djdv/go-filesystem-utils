package command_test

import (
	"context"
	"errors"
	"flag"
	"io"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/command"
)

const (
	synopisSuffix = " Synopis"
	usageSuffix   = " Usage"

	noopName     = "noop"
	noopSynopsis = noopName + synopisSuffix
	noopUsage    = noopName + usageSuffix

	noopArgsName     = "noopArgs"
	noopArgsSynopsis = noopArgsName + synopisSuffix
	noopArgsUsage    = noopArgsName + usageSuffix

	subsName     = "subcommands"
	subsSynopsis = subsName + synopisSuffix
	subsUsage    = subsName + usageSuffix

	noopSubName = "subcommand"
	subSynopsis = noopSubName + synopisSuffix
	subUsage    = noopSubName + usageSuffix

	noopDiscardName     = "noopDiscardName"
	noopDiscardArgsName = "noopDiscardArgsName"
)

type (
	settings struct {
		command.HelpArg
		someField bool
	}

	// cmdMap maps the name of commands to their respective objects
	cmdMap map[string]command.Command
)

var (
	testCommands = cmdMap{
		noopName: command.MakeCommand[*settings](
			noopName, noopSynopsis, noopUsage, noop,
		),
		noopArgsName: command.MakeCommand[*settings](
			noopArgsName, noopArgsSynopsis, noopArgsUsage, noopArgs,
		),
		noopSubName: command.MakeCommand[*settings](
			noopSubName, subSynopsis, subUsage, noop,
		),
	}

	discard = io.Discard.(command.StringWriter)

	testInvalidCommands = cmdMap{
		noopDiscardName: command.MakeCommand[*settings](
			noopDiscardName, noopSynopsis, noopUsage, noop,
			command.WithUsageOutput(discard),
		),
		noopDiscardArgsName: command.MakeCommand[*settings](
			noopDiscardArgsName, noopArgsSynopsis, noopArgsUsage, noopArgs,
			command.WithUsageOutput(discard),
		),
	}
)

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

func TestCommand(t *testing.T) {
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

func (ts *settings) BindFlags(fs *flag.FlagSet) {
	ts.HelpArg.BindFlags(fs)
	fs.BoolVar(&ts.someField, "sf", false, "Some Flag")
}

func exeValid(t *testing.T) {
	t.Parallel()

	cmds := copyCmds(testCommands)

	cmds[subsName] = command.MakeCommand[*settings](
		subsName, subsSynopsis, subsUsage, noopArgs,
		command.WithSubcommands(testCommands[noopSubName]),
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
			[]string{noopSubName},
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

	cmds := copyCmds(testInvalidCommands)

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
