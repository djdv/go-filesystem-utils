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

const (
	synopisSuffix = " Synopis"
	usageSuffix   = " Usage"

	noopName       = "noop"
	noopArgsName   = "noopArgs"
	parentName     = "noopSenior"
	childName      = "noopJunior"
	grandChildName = "noopIII"
)

type (
	settings struct {
		command.HelpArg
		someField bool
	}

	cmdMap map[string]command.Command
)

func noopCmds() cmdMap {
	const (
		noopSynopsis = noopName + synopisSuffix
		noopUsage    = noopName + usageSuffix

		noopArgsSynopsis = noopArgsName + synopisSuffix
		noopArgsUsage    = noopArgsName + usageSuffix
	)
	return cmdMap{
		noopName: command.MakeCommand[*settings](
			noopName, noopSynopsis, noopUsage, noop,
		),
		noopArgsName: command.MakeCommand[*settings](
			noopArgsName, noopArgsSynopsis, noopArgsUsage, noopArgs,
		),
		parentName: command.MakeCommand[*settings](
			parentName, noopSynopsis, noopUsage, noop,
			command.WithSubcommands(
				command.MakeCommand[*settings](
					childName, noopArgsSynopsis, noopArgsUsage, noopArgs,
					command.WithSubcommands(
						command.MakeCommand[*settings](
							grandChildName, noopSynopsis, noopUsage, noop,
						),
					),
				),
			),
		),
	}
}

func (ts *settings) BindFlags(fs *flag.FlagSet) {
	ts.HelpArg.BindFlags(fs)
	fs.BoolVar(&ts.someField, "sf", false, "Some Flag")
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
	const (
		name     = "subcommands"
		synopsis = name + synopisSuffix
		usage    = name + usageSuffix
	)
	var (
		noopCmds = noopCmds()
		execFn   = noop
		cmd      = command.MakeCommand[*settings](
			name, synopsis, usage, execFn,
			command.WithSubcommands(noopCmds[noopName]),
			command.WithSubcommands(noopCmds[noopArgsName]),
		)
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
	cmds := noopCmds()
	cmds[subsName] = command.MakeCommand[*settings](
		subsName, subsSynopsis, subsUsage, noop,
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
		{
			noopName,
			[]string{"-sf=true"},
		},
		{
			parentName,
			[]string{childName, "arg"},
		},
		{
			parentName,
			[]string{childName, grandChildName},
		},
	} {
		var (
			name = test.name
			args = test.args
		)
		t.Run(fmt.Sprint(name, args), func(t *testing.T) {
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
	const (
		name     = "testcmd"
		synopsis = name + synopisSuffix
		usage    = name + usageSuffix
	)
	var (
		discard = io.Discard.(command.StringWriter)
		execFn  = noop
		cmds    = cmdMap{
			name: command.MakeCommand[*settings](
				name, synopsis, usage, execFn,
				command.WithUsageOutput(discard),
			),
		}
	)
	const niladicFuncWithArgs = "niladic function called with args"
	for _, test := range []struct {
		name     string
		args     []string
		expected error
		reason   string
	}{
		{
			name,
			[]string{"arg1", "arg2"},
			command.ErrUsage,
			niladicFuncWithArgs,
		},
		{
			name,
			[]string{childName, grandChildName, "arg"},
			command.ErrUsage,
			niladicFuncWithArgs,
		},
		{
			name,
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
