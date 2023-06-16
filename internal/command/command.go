package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"golang.org/x/term"
)

type (
	// Command is a decorated function ready to be executed.
	Command interface {
		// Name returns a human friendly name of the command,
		// which may be used to identify commands
		// as well as decorate user facing help-text.
		Name() string

		// Synopsis returns a single-line short string describing the command.
		Synopsis() string

		// Usage returns an arbitrarily long string explaining how to use the command.
		Usage() string

		// Subcommands returns a list of subcommands (if any).
		Subcommands() []Command

		// Execute executes the command, with or without any arguments.
		Execute(ctx context.Context, args ...string) error
	}
	// ExecuteType is a constraint that permits any reference
	// type that can bind its value(s) to flags.
	ExecuteType[T any] interface {
		*T
		FlagBinder
	}
	// A FlagBinder should call relevant [flag.FlagSet] methods
	// to bind each of it's variable references with the FlagSet.
	// E.g. a struct would pass references of its fields
	// to `FlagSet.Var(&structABC.fieldXYZ, ...)`.
	FlagBinder interface {
		BindFlags(*flag.FlagSet)
	}
	// Option is a functional option.
	// One can be returned by the various constructors
	// before being passed to [MakeCommand].
	Option        func(*commandCommon)
	commandCommon struct {
		name, synopsis, usage string
		usageOutput           io.Writer
		subcommands           []Command
		glamour               bool
	}

	// UsageError may be returned by commands
	// to signal that its usage string should
	// be presented to the caller.
	UsageError struct{ Err error }

	writeStringFunc   func(string)
	stringModiferFunc func(string) string
)

func (ue UsageError) Error() string { return ue.Err.Error() }

// Unwrap implements the [errors.Unwrap] interface.
func (ue UsageError) Unwrap() error { return ue.Err }

func unexpectedArguments(name string, args []string) UsageError {
	return UsageError{
		Err: fmt.Errorf(
			"`%s` does not take arguments but was provided: %s",
			name, strings.Join(args, ","),
		),
	}
}

// WithSubcommands provides a command with subcommands.
// Subcommands will be called if the supercommand receives
// arguments that match the subcommand name.
func WithSubcommands(subcommands ...Command) Option {
	return func(settings *commandCommon) {
		settings.subcommands = subcommands
	}
}

// WithUsageOutput sets the writer that is written
// to when [Command.Execute] receives a request for
// help, or returns [UsageError].
func WithUsageOutput(output io.Writer) Option {
	return func(settings *commandCommon) {
		settings.usageOutput = output
	}
}

// SubcommandGroup returns a command that only defers to subcommands.
// Trying to execute the command itself will return [UsageError].
func SubcommandGroup(name, synopsis string, subcommands []Command, options ...Option) Command {
	const usage = "Must be called with a subcommand."
	return MakeNiladicCommand(name, synopsis, usage,
		func(context.Context) error {
			return UsageError{
				Err: fmt.Errorf(
					"`%s` only accepts subcommands", name,
				),
			}
		},
		append(options, WithSubcommands(subcommands...))...,
	)
}

func (cmd *commandCommon) Name() string           { return cmd.name }
func (cmd *commandCommon) Synopsis() string       { return cmd.synopsis }
func (cmd *commandCommon) Usage() string          { return cmd.usage }
func (cmd *commandCommon) Subcommands() []Command { return generic.CloneSlice(cmd.subcommands) }

func newFlagSet(name string) *flag.FlagSet {
	return flag.NewFlagSet(name, flag.ContinueOnError)
}

func (cmd *commandCommon) parseFlags(flagSet *flag.FlagSet, arguments ...string) (bool, error) {
	var needHelp bool
	bindHelpFlag(&needHelp, flagSet)
	bindRenderFlag(&cmd.glamour, flagSet)
	// Package [flag] has implicit handling for `-help` and `-h` flags.
	// If they're not explicitly defined, but provided as arguments,
	// [flag] will call `Usage` before returning from `Parse`.
	// We want to disable any built-in printing, to assure
	// our printers are used exclusively. (For both help text and errors)
	flagSet.Usage = func() { /* NOOP */ }
	flagSet.SetOutput(io.Discard)
	err := flagSet.Parse(arguments)
	if err == nil {
		return needHelp, nil
	}
	if errors.Is(err, flag.ErrHelp) {
		needHelp = true
		return needHelp, nil
	}
	return needHelp, UsageError{Err: err}
}

func bindHelpFlag(value *bool, flagSet *flag.FlagSet) {
	const (
		helpName    = "help"
		helpUsage   = "prints out this help text"
		helpDefault = false
	)
	flagSet.BoolVar(value, helpName, helpDefault, helpUsage)
}

func bindRenderFlag(value *bool, flagSet *flag.FlagSet) {
	const (
		renderName    = "video-terminal"
		renderUsage   = "render text for video terminals"
		renderDefault = true
	)
	flagSet.BoolVar(value, renderName, renderDefault, renderUsage)
}

func getSubcommand(command Command, arguments []string) (Command, []string) {
	if len(arguments) == 0 {
		return nil, nil
	}
	subname := arguments[0]
	for _, subcommand := range command.Subcommands() {
		if subcommand.Name() != subname {
			continue
		}
		subarguments := arguments[1:]
		if hypoCmd, hypoArgs := getSubcommand(subcommand, subarguments); hypoCmd != nil {
			return hypoCmd, hypoArgs
		}
		return subcommand, subarguments
	}
	return nil, nil
}

func (cmd *commandCommon) maybePrintUsage(err error, acceptsArgs bool, flagSet *flag.FlagSet) error {
	var usageErr UsageError
	if !errors.Is(err, flag.ErrHelp) &&
		!errors.As(err, &usageErr) {
		return err
	}
	if printErr := cmd.printUsage(acceptsArgs, flagSet); printErr != nil {
		return printErr
	}
	return err
}

func (cmd *commandCommon) printUsage(acceptsArgs bool, flagSet *flag.FlagSet) error {
	var (
		output     = cmd.usageOutput
		wantStyled = cmd.glamour
		renderer   *glamour.TermRenderer
	)
	if output == nil {
		output, wantStyled = newDefaultOutput(wantStyled)
	}
	if wantStyled {
		var err error
		if renderer, err = newRenderer(); err != nil {
			return err
		}
	}
	var (
		wErr    error
		writeFn = func(text string) {
			if wErr != nil {
				return
			}
			_, wErr = io.WriteString(output, text)
		}
		name        = cmd.name
		usage       = cmd.usage
		subcommands = cmd.subcommands
		hasSubs     = len(subcommands) > 0
		hasFlags    bool
	)
	flagSet.VisitAll(func(*flag.Flag) { hasFlags = true })
	printUsage(writeFn, usage, renderer)
	printCommandLine(writeFn, name, hasSubs, hasFlags, acceptsArgs, renderer)
	if hasFlags {
		printFlags(writeFn, flagSet, renderer)
	}
	if hasSubs {
		printSubcommands(writeFn, subcommands, renderer)
	}
	return wErr
}

func newDefaultOutput(withStyle bool) (_ io.Writer, supportsANSI bool) {
	stderr := os.Stderr
	if withStyle {
		if term.IsTerminal(int(stderr.Fd())) {
			return ansiStderr(), true
		}
	}
	return stderr, false
}

func mustRender(renderer *glamour.TermRenderer, text string) string {
	render, err := renderer.Render(text)
	if err != nil {
		panic(err)
	}
	return strings.TrimSpace(render)
}

func printUsage(
	writeFn writeStringFunc,
	usage string, renderer *glamour.TermRenderer,
) {
	if renderer != nil {
		usage = mustRender(renderer, usage)
	}
	writeFn(usage + "\n\n")
}

func printCommandLine(
	writeFn writeStringFunc,
	name string,
	hasSubcommands, hasFlags, acceptsArgs bool,
	renderer *glamour.TermRenderer,
) {
	var (
		usageText string
		styled    = renderer != nil
		render    stringModiferFunc
	)
	if styled {
		render = func(text string) string {
			return mustRender(renderer, text)
		}
		usageText = render("Usage:") +
			"\n\t" + render(bold(name))
	} else {
		usageText = "Usage:\n\t" + name
	}
	writeFn(usageText)
	if hasSubcommands {
		subcommandText := "subcommand"
		if styled {
			subcommandText = render(subcommandText)
		}
		writeFn(" " + subcommandText)
	}
	if hasFlags {
		var flagsText string
		if styled {
			// NOTE: this could be done in a single pass
			// but "**[**flags***]**" confuses glamour,
			// and using underscores is discouraged.
			flagsText = render(bold("[")) +
				render(italic("flags")) +
				render(bold("]"))
		} else {
			flagsText = "[flags]"
		}
		writeFn(" " + flagsText)
	}
	if acceptsArgs {
		argumentsText := "...arguments"
		if styled {
			argumentsText = render(italic(argumentsText))
		}
		writeFn(" " + argumentsText)
	}
	writeFn("\n")
}

// *modification of standard [flag]'s implementation.
func printFlags(
	writeFn writeStringFunc,
	flagSet *flag.FlagSet,
	renderer *glamour.TermRenderer,
) {
	var (
		flagText                = "Flags:"
		styled                  = renderer != nil
		render, italicUnderline stringModiferFunc
	)
	if styled {
		render = func(text string) string {
			return mustRender(renderer, text)
		}
		italicUnderline = newItalicUnderlineRenderer(renderer)
		flagText = render("Flags:")
	}
	writeFn(flagText + "\n")
	flagSet.VisitAll(func(flg *flag.Flag) {
		const singleCharName = 2
		var (
			flagName  = "-" + flg.Name
			shortFlag = len(flagName) == singleCharName
		)
		if styled {
			flagName = render(bold(flagName))
		}
		writeFn("  " + flagName)
		valueType, usage := flag.UnquoteUsage(flg)
		if len(valueType) > 0 {
			if styled {
				valueType = italicUnderline(valueType)
			}
			writeFn(" " + valueType)
		}
		if shortFlag {
			writeFn("\t")
		} else {
			writeFn("\n    \t")
		}
		if styled {
			usage = render(usage)
		}
		writeFn(strings.ReplaceAll(usage, "\n", "\n    \t"))
		if defaultText := flg.DefValue; !isZeroValue(flg, defaultText) {
			const prefix, suffix = "(default: ", ")"
			if styled {
				if !strings.Contains(defaultText, "`") {
					defaultText = "`" + defaultText + "`"
				}
				defaultText = render(prefix + defaultText + suffix)
			} else {
				defaultText = prefix + defaultText + suffix
			}
			writeFn("\n    \t" + defaultText)
		}
		writeFn("\n")
	})
}

// HACK: Markdown doesn't have syntax for underline,
// but we can render it manually since ANSI supports it.
func newItalicUnderlineRenderer(renderer *glamour.TermRenderer) stringModiferFunc {
	var (
		style = _extractStyle(renderer)
		ctx   = ansi.NewRenderContext(ansi.Options{})
		verum = true
		color *string
	)
	if textColor := style.Text.Color; textColor != nil {
		color = textColor
	} else if documentColor := style.Document.Color; documentColor != nil {
		color = documentColor
	}
	var (
		builder strings.Builder
		element = &ansi.BaseElement{
			Style: ansi.StylePrimitive{
				Color:     color,
				Italic:    &verum,
				Underline: &verum,
			},
		}
	)
	return func(text string) string {
		element.Token = text
		if err := element.Render(&builder, ctx); err != nil {
			panic(err)
		}
		render := builder.String()
		builder.Reset()
		return render
	}
}

// isZeroValue determines whether the string represents the zero
// value for a flag.
// *Borrowed from standard library.
func isZeroValue(flg *flag.Flag, value string) bool {
	// Build a zero value of the flag's Value type, and see if the
	// result of calling its String method equals the value passed in.
	// This works unless the Value type is itself an interface type.
	typ := reflect.TypeOf(flg.Value)
	var zero reflect.Value
	if typ.Kind() == reflect.Pointer {
		zero = reflect.New(typ.Elem())
	} else {
		zero = reflect.Zero(typ)
	}
	// Deviation from standard; if the code is wrong
	// let this panic. Package [flag] explicitly requires
	// [flag.Value] to have a `String` method that is
	// callable with a `nil` receiver.
	return value == zero.Interface().(flag.Value).String()
}

func printSubcommands(writeFn writeStringFunc, subcommands []Command, renderer *glamour.TermRenderer) {
	var (
		subcommandsText = "Subcommands:"
		styled          = renderer != nil
		render          stringModiferFunc
	)
	if styled {
		render = func(text string) string {
			return mustRender(renderer, text)
		}
		subcommandsText = render(subcommandsText)
	}
	writeFn(subcommandsText + "\n")
	const (
		minWidth = 0
		tabWidth = 0
		padding  = 0
		padChar  = ' '
		flags    = 0
	)
	var (
		subcommandsBuffer strings.Builder
		tabWriter         = tabwriter.NewWriter(
			&subcommandsBuffer, minWidth, tabWidth, padding, padChar, flags,
		)
	)
	for _, subcommand := range subcommands {
		if _, err := fmt.Fprintf(tabWriter,
			"  %s\t - %s\n", // 2 leading spaces to match [flag] behaviour.
			subcommand.Name(), subcommand.Synopsis(),
		); err != nil {
			panic(err)
		}
	}
	if err := tabWriter.Flush(); err != nil {
		panic(err)
	}
	subcommandsTable := subcommandsBuffer.String()
	if styled {
		subcommandsTable = render(subcommandsTable)
	}
	writeFn(subcommandsTable + "\n")
}

func applyOptions(settings *commandCommon, options ...Option) {
	for _, apply := range options {
		apply(settings)
	}
}
