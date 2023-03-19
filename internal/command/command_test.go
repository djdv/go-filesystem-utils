package command_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/command"
)

const (
	noopName     = "noop"
	noopArgsName = "noopArgs"
)

type cmdMap map[string]command.Command

func noopCmds() cmdMap {
	const (
		noopSynopsis = noopName + synopisSuffix
		noopUsage    = noopName + usageSuffix

		noopArgsSynopsis = noopArgsName + synopisSuffix
		noopArgsUsage    = noopArgsName + usageSuffix
	)
	return cmdMap{
		noopName: command.MustMakeCommand[*exampleSettings](
			noopName, noopSynopsis, noopUsage, noop,
		),
		noopArgsName: command.MustMakeCommand[*exampleSettings](
			noopArgsName, noopArgsSynopsis, noopArgsUsage, noopArgs,
		),
		parentName: command.MustMakeCommand[*exampleSettings](
			parentName, noopSynopsis, noopUsage, noop,
			command.WithSubcommands(
				command.MustMakeCommand[*exampleSettings](
					childName, noopArgsSynopsis, noopArgsUsage, noopArgs,
					command.WithSubcommands(
						command.MustMakeCommand[*exampleSettings](
							grandChildName, noopSynopsis, noopUsage, noop,
						),
					),
				),
			),
		),
	}
}

func noop(ctx context.Context, set *exampleSettings) error {
	return nil
}

func noopArgs(ctx context.Context, set *exampleSettings, args ...string) error {
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
		cmd      = command.MustMakeCommand[*exampleSettings](
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
	cmds[subsName] = command.MustMakeCommand[*exampleSettings](
		subsName, subsSynopsis, subsUsage, noop,
		command.WithSubcommands(
			command.MustMakeCommand[*exampleSettings](
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
			[]string{"-" + someFlagName + "=true"},
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
			name: command.MustMakeCommand[*exampleSettings](
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
