package command_test

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/command"
)

func TestCommand(t *testing.T) {
	t.Parallel()
	t.Run("niladic", cmdNiladic)
	t.Run("fixed", cmdFixed)
	t.Run("variadic", cmdVariadic)
	t.Run("subcommands", cmdSubcommands)
	t.Run("options", cmdOptions)
}

func testHelpText(t *testing.T, cmd command.Command) {
	t.Helper()
	name := cmd.Name()
	if len(name) == 0 {
		t.Errorf(
			"command did not identify itself by name: %T",
			cmd,
		)
	}
	synopsis := cmd.Synopsis()
	if len(synopsis) == 0 {
		t.Errorf(
			`command "%s" did not return a synopsis`,
			name,
		)
	}
	usage := cmd.Usage()
	if len(usage) == 0 {
		t.Errorf(
			`command "%s" did not return usage help text`,
			name,
		)
	}
}

func testErrorParameters(t *testing.T, cmd command.Command) {
	t.Helper()
	const usageMessage = "expected `ErrUsage`"
	ctx := context.Background()
	for _, test := range []struct {
		arguments []string
		message   string
	}{
		{
			arguments: []string{"-help"},
			message:   usageMessage,
		},
		{
			arguments: []string{"-h"},
			message:   usageMessage,
		},
		{
			arguments: []string{"-invalid"},
			message:   usageMessage,
		},
		{
			arguments: []string{"some", "arguments"},
			message:   usageMessage,
		},
	} {
		if err := cmd.Execute(ctx, test.arguments...); err == nil {
			t.Error(test.message)
		}
	}
}

func cmdNiladic(t *testing.T) {
	t.Parallel()
	t.Run("help text", nilCmd)
	t.Run("valid", nilValid)
	t.Run("invalid", nilInvalid)
}

func nilCmd(t *testing.T) {
	t.Parallel()
	cmd := newNiladicTestCommand(t)
	testHelpText(t, cmd)
}

func newNiladicTestCommand(t *testing.T) command.Command {
	t.Helper()
	const (
		name     = "niladic"
		synopsis = "Prints a message."
		usage    = "Call the command with no arguments"
	)
	var (
		discard  = io.Discard.(command.StringWriter)
		cmd, err = command.MakeNiladicCommand(
			name, synopsis, usage,
			func(context.Context) error { return nil },
			command.WithUsageOutput(discard),
		)
	)
	if err != nil {
		t.Fatal(err)
	}
	return cmd
}

func nilValid(t *testing.T) {
	t.Parallel()
	var (
		cmd = newNiladicTestCommand(t)
		ctx = context.Background()
	)
	if err := cmd.Execute(ctx); err != nil {
		t.Error(err)
	}
}

func nilInvalid(t *testing.T) {
	t.Parallel()
	var (
		cmd = newNiladicTestCommand(t)
		ctx = context.Background()
	)
	const usageMessage = "expected ErrUsage"
	for _, test := range []struct {
		arguments []string
		message   string
	}{
		{
			arguments: []string{"-help"},
			message:   usageMessage,
		},
		{
			arguments: []string{"-h"},
			message:   usageMessage,
		},
		{
			arguments: []string{"-invalid"},
			message:   usageMessage,
		},
		{
			arguments: []string{"some", "arguments"},
			message:   usageMessage,
		},
	} {
		if err := cmd.Execute(ctx, test.arguments...); err == nil {
			t.Error(test.message)
		}
	}
}

func cmdFixed(t *testing.T) {
	t.Parallel()
	t.Run("help text", fixedCmd)
	t.Run("valid", fixedValid)
	t.Run("invalid", fixedInvalid)
}

func fixedCmd(t *testing.T) {
	t.Parallel()
	var (
		cmd, _        = newFixedTestCommand(t)
		cmdArgs, _, _ = newFixedArgsTestCommand(t)
	)
	testHelpText(t, cmd)
	testHelpText(t, cmdArgs)
}

func newFixedTestCommand(t *testing.T) (command.Command, *fixedType) {
	t.Helper()
	const (
		name     = "fixed"
		synopsis = "Prints a value."
		usage    = "Call the command with or" +
			" without flags"
		flagDefault = 1
	)
	var (
		fixed = &fixedType{
			someField: flagDefault,
		}
		discard  = io.Discard.(command.StringWriter)
		cmd, err = command.MakeFixedCommand[*fixedType](
			name, synopsis, usage,
			func(_ context.Context, settings *fixedType) error {
				*fixed = *settings
				return nil
			},
			command.WithUsageOutput(discard),
		)
	)
	if err != nil {
		t.Fatal(err)
	}
	return cmd, fixed
}

func newFixedArgsTestCommand(t *testing.T) (command.Command, *fixedType, *[]string) {
	t.Helper()
	const (
		name     = "fixed"
		synopsis = "Prints a value."
		usage    = "Call the command with or" +
			" without flags"
		flagDefault = 1
	)
	var (
		fixed = &fixedType{
			someField: flagDefault,
		}
		args     = new([]string)
		discard  = io.Discard.(command.StringWriter)
		cmd, err = command.MakeFixedCommand[*fixedType](
			name, synopsis, usage,
			func(_ context.Context, settings *fixedType, arguments ...string) error {
				*args = arguments
				*fixed = *settings
				return nil
			},
			command.WithUsageOutput(discard),
		)
	)
	if err != nil {
		t.Fatal(err)
	}
	return cmd, fixed, args
}

func fixedValid(t *testing.T) {
	t.Parallel()
	t.Run("flags", fixedValidFlags)
	t.Run("arguments", fixedValidArguments)
}

func fixedValidFlags(t *testing.T) {
	t.Parallel()
	var (
		cmd, settings = newFixedTestCommand(t)
		ctx           = context.Background()
		settingsPre   = *settings
	)
	if err := cmd.Execute(ctx); err != nil {
		t.Error(err)
	}
	if got := *settings; got != settingsPre {
		t.Errorf(
			"no arguments provided but settings changed from defaults"+
				"\n\tgot: %#v"+
				"\n\twant: %#v",
			got, settingsPre,
		)
	}
	if err := cmd.Execute(ctx, "-flag=2"); err != nil {
		t.Error(err)
	}
	want := settingsPre
	want.someField = 2
	if got := *settings; got != want {
		t.Errorf(
			"arguments provided but settings did not changed from defaults"+
				"\n\tgot: %#v"+
				"\n\twant: %#v",
			*settings, settingsPre,
		)
	}
}

func fixedValidArguments(t *testing.T) {
	t.Parallel()
	var (
		cmd, settings, arguments = newFixedArgsTestCommand(t)
		ctx                      = context.Background()
		settingsPre              = *settings
	)
	if err := cmd.Execute(ctx); err != nil {
		t.Error(err)
	}
	if got := *settings; got != settingsPre {
		t.Errorf(
			"no arguments provided but settings changed from defaults"+
				"\n\tgot: %#v"+
				"\n\twant: %#v",
			got, settingsPre,
		)
	}
	if err := cmd.Execute(ctx, "-flag=2"); err != nil {
		t.Error(err)
	}
	want := settingsPre
	want.someField = 2
	if got := *settings; got != want {
		t.Errorf(
			"arguments provided but settings did not changed from defaults"+
				"\n\tgot: %#v"+
				"\n\twant: %#v",
			*settings, settingsPre,
		)
	}
	wantArguments := []string{"a", "b", "c"}
	if err := cmd.Execute(ctx, wantArguments...); err != nil {
		t.Error(err)
	}
	if got := *arguments; !reflect.DeepEqual(got, wantArguments) {
		t.Errorf(
			"arguments provided but vector did not change"+
				"\n\tgot: %#v"+
				"\n\twant: %#v",
			got, wantArguments,
		)
	}
}

func fixedInvalid(t *testing.T) {
	t.Parallel()
	cmd, _ := newFixedTestCommand(t)
	testErrorParameters(t, cmd)
}

func cmdVariadic(t *testing.T) {
	t.Parallel()
	t.Run("help text", variadicCmd)
	t.Run("valid", variadicValid)
	t.Run("invalid", variadicInvalid)
}

func variadicCmd(t *testing.T) {
	t.Parallel()
	var (
		cmd, _        = newVariadicTestCommand(t)
		cmdArgs, _, _ = newVariadicArgsTestCommand(t)
	)
	testHelpText(t, cmd)
	testHelpText(t, cmdArgs)
}

func newVariadicTestCommand(t *testing.T) (command.Command, *settings) {
	const (
		name     = "variadic"
		synopsis = "Prints a value."
		usage    = "Call the command with or" +
			" without flags"
	)
	var (
		settings = settings{
			someField: variadicFlagDefault,
		}
		discard  = io.Discard.(command.StringWriter)
		cmd, err = command.MakeVariadicCommand[options](
			name, synopsis, usage,
			func(ctx context.Context, options ...option) error {
				for _, apply := range options {
					if err := apply(&settings); err != nil {
						return err
					}
				}
				return nil
			},
			command.WithUsageOutput(discard),
		)
	)
	if err != nil {
		t.Fatal(err)
	}
	return cmd, &settings
}

func newVariadicArgsTestCommand(t *testing.T) (command.Command, *settings, *[]string) {
	const (
		name     = "fixed-args"
		synopsis = "Prints a value and arguments."
		usage    = "Call the command with or" +
			" without flags or arguments"
	)
	var (
		args     = new([]string)
		settings = settings{
			someField: variadicFlagDefault,
		}
		discard  = io.Discard.(command.StringWriter)
		cmd, err = command.MakeVariadicCommand[options](
			name, synopsis, usage,
			func(ctx context.Context, arguments []string, options ...option) error {
				for _, apply := range options {
					if err := apply(&settings); err != nil {
						return err
					}
				}
				*args = arguments
				return nil
			},
			command.WithUsageOutput(discard),
		)
	)
	if err != nil {
		t.Fatal(err)
	}
	return cmd, &settings, args
}

func variadicValid(t *testing.T) {
	t.Parallel()
	t.Run("flags", variadicValidFlags)
	t.Run("arguments", variadicValidArguments)
}

func variadicValidFlags(t *testing.T) {
	t.Parallel()
	var (
		cmd, settings = newVariadicTestCommand(t)
		ctx           = context.Background()
		settingsPre   = *settings
	)
	if err := cmd.Execute(ctx); err != nil {
		t.Error(err)
	}
	if got := *settings; got != settingsPre {
		t.Errorf(
			"no arguments provided but settings changed from defaults"+
				"\n\tgot: %#v"+
				"\n\twant: %#v",
			got, settingsPre,
		)
	}
	if err := cmd.Execute(ctx, "-flag=2"); err != nil {
		t.Error(err)
	}
	want := settingsPre
	want.someField = 2
	if got := *settings; got != want {
		t.Errorf(
			"arguments provided but settings did not changed from defaults"+
				"\n\tgot: %#v"+
				"\n\twant: %#v",
			*settings, settingsPre,
		)
	}
}

func variadicValidArguments(t *testing.T) {
	t.Parallel()
	var (
		cmd, settings, arguments = newVariadicArgsTestCommand(t)
		ctx                      = context.Background()
		settingsPre              = *settings
	)
	if err := cmd.Execute(ctx); err != nil {
		t.Error(err)
	}
	if got := *settings; got != settingsPre {
		t.Errorf(
			"no arguments provided but settings changed from defaults"+
				"\n\tgot: %#v"+
				"\n\twant: %#v",
			got, settingsPre,
		)
	}
	if err := cmd.Execute(ctx, "-flag=2"); err != nil {
		t.Error(err)
	}
	want := settingsPre
	want.someField = 2
	if got := *settings; got != want {
		t.Errorf(
			"arguments provided but settings did not changed from defaults"+
				"\n\tgot: %#v"+
				"\n\twant: %#v",
			*settings, settingsPre,
		)
	}
	wantArguments := []string{"a", "b", "c"}
	if err := cmd.Execute(ctx, wantArguments...); err != nil {
		t.Error(err)
	}
	if got := *arguments; !reflect.DeepEqual(got, wantArguments) {
		t.Errorf(
			"arguments provided but vector did not change"+
				"\n\tgot: %#v"+
				"\n\twant: %#v",
			got, wantArguments,
		)
	}
}

func variadicInvalid(t *testing.T) {
	t.Parallel()
	cmd, _ := newVariadicTestCommand(t)
	testErrorParameters(t, cmd)
}

func cmdSubcommands(t *testing.T) {
	t.Parallel()
	t.Run("help text", subcommandCmd)
	t.Run("valid", subcommandValid)
	t.Run("invalid", subcommandInvalid)
}

func newTestSubcommands(t *testing.T) command.Command {
	var (
		noopFn          = func(context.Context) error { return nil }
		mustMakeCommand = func(name string) command.Command {
			var (
				synopsis = name + " synopsis"
				usage    = name + " usage"
				cmd, err = command.MakeNiladicCommand(
					name, synopsis, usage, noopFn,
				)
			)
			if err != nil {
				t.Fatal(err)
			}
			return cmd
		}
		discard    = io.Discard.(command.StringWriter)
		cmdOptions = []command.Option{
			command.WithUsageOutput(discard),
		}
		cmd = command.SubcommandGroup(
			"top", "Top level group",
			[]command.Command{
				command.SubcommandGroup(
					"A", "middle group 1",
					[]command.Command{
						mustMakeCommand("1"),
					},
					cmdOptions...,
				),
				command.SubcommandGroup(
					"B", "middle group 2",
					[]command.Command{
						mustMakeCommand("2"),
					},
					cmdOptions...,
				),
			},
			cmdOptions...,
		)
	)
	return cmd
}

func subcommandCmd(t *testing.T) {
	t.Parallel()
	cmd := newTestSubcommands(t)
	testHelpText(t, cmd)
}

func subcommandValid(t *testing.T) {
	t.Parallel()
	var (
		ctx      = context.Background()
		discard  = io.Discard.(command.StringWriter)
		groupCmd = newTestSubcommands(t)
	)
	const (
		niladicName  = "niladic"
		fixedName    = "fixed"
		variadicName = "variadic"
		synopsis     = ""
		usage        = synopsis
	)
	nilCmd, err := command.MakeNiladicCommand(
		niladicName, synopsis, usage,
		func(context.Context) error { return nil },
		command.WithUsageOutput(discard),
		command.WithSubcommands(groupCmd),
	)
	if err != nil {
		t.Fatal(err)
	}
	fixedCmd, err := command.MakeFixedCommand[*fixedType](
		fixedName, synopsis, usage,
		func(context.Context, *fixedType) error { return nil },
		command.WithUsageOutput(discard),
		command.WithSubcommands(groupCmd),
	)
	variadicCmd, err := command.MakeVariadicCommand[options](
		variadicName, synopsis, usage,
		func(context.Context, ...option) error { return nil },
		command.WithUsageOutput(discard),
		command.WithSubcommands(groupCmd),
	)
	if err != nil {
		t.Fatal(err)
	}
	subnames := [][]string{
		{"A", "1"},
		{"B", "2"},
	}
	for i, arguments := range subnames {
		if err := groupCmd.Execute(ctx, arguments...); err != nil {
			t.Error(err)
		}
		subnames[i] = append([]string{groupCmd.Name()}, arguments...)
	}
	for _, cmd := range []command.Command{
		nilCmd, fixedCmd, variadicCmd,
	} {
		for _, arguments := range subnames {
			if err := cmd.Execute(ctx, arguments...); err != nil {
				t.Error(err)
			}
		}
	}
}

func subcommandInvalid(t *testing.T) {
	t.Parallel()
	var (
		cmd = newTestSubcommands(t)
		ctx = context.Background()
	)
	if err := cmd.Execute(ctx); err == nil {
		t.Error(
			"subcommand group is expected to return `ErrUsage`" +
				"when called directly, but returned nil",
		)
	}
	testErrorParameters(t, cmd)
}

func cmdOptions(t *testing.T) {
	t.Parallel()
	const (
		name     = "ghost"
		synopsis = "can't print."
		usage    = "Call the command with no arguments"
	)
	var (
		ctx      = context.Background()
		cmd, err = command.MakeNiladicCommand(
			name, synopsis, usage,
			func(context.Context) error { return nil },
			command.WithUsageOutput(nil),
		)
	)
	if err != nil {
		t.Fatal(err)
	}
	testHelpText(t, cmd)
	want := command.ErrUsage
	if err := cmd.Execute(ctx, "-help"); !errors.Is(err, want) {
		t.Errorf(
			`command "%s" returned unexpected error`+
				"\n\tgot: %v"+
				"\n\twant: %v",
			name, err, want,
		)
	}
}
