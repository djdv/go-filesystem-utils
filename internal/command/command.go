package command

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
)

// TODO: name and docs
type (
	// Can/must be embedded into any command flag structure.
	// Automatically implements a check that looks for `-help`
	// and returns before calling ExecuteFunc
	HelpFlag bool

	// Settings is a constraint that permits any reference type
	// which also implements a [FlagBinder] and the help flag hook method.
	Settings[settings any] interface {
		*settings
		FlagBinder
		HelpRequested() bool // TODO: Break out into distinct interface?
	}

	// the primary expected signature of a command's Execute function/method.
	ExecuteFunc[settings Settings[sTyp], sTyp any] func(
		context.Context, settings, ...string) error

	// typical signature of [ExecuteFunc] after it's unique types have been elided.
	wrappedExecuteFunc func(context.Context, ...string) error

	command struct {
		name, synopsis string
		usage          func() string
		execute        wrappedExecuteFunc
		subcommands    []Command
		niladic        bool
	}
)

// TODO: lint; not (yet?) allowed by the spec
// https://github.com/golang/go/issues/48522
// func Helper[s struct{ HelpFlag }]() { }
// We have to use dynamic dispatch instead. See use of [HelpRequested] method.

// TODO: name + docs
// This binds the HelpFlag type to a flagset.
// Currently this is just a bool, but could in theory change later.
// (E.g. instead of just "help" we can distinguish short `-h` / long `-help` etc. )
// With a constructor like this caller's won't need to worry about change.
func NewHelpFlag(fs *flag.FlagSet, b *HelpFlag) {
	const usage = "Prints out this help text."
	fs.BoolVar((*bool)(b), "help", false, usage)
	// TODO: consider aliasing this.
	// With either different behaviour "short help" convention.
	// or conflated into 1 flag, but this requires special aliasing rules when printing.
	// (Otherwise it shows up twice since it's 2 distinct flags, aliasing 1 value.)
	// fs.BoolVar((*bool)(b), "h", false, usage)
}

func (b HelpFlag) HelpRequested() bool { return bool(b) }

// Returns string representation of HelpFlag's boolean value
func (b *HelpFlag) String() string { return strconv.FormatBool(bool(*b)) }

// Parse boolean value from string representation and set to HelpFlag
func (b *HelpFlag) Set(str string) error {
	val, err := strconv.ParseBool(str)
	if err != nil {
		return err
	}
	*b = HelpFlag(val)
	return nil
}

func MakeCommand[sPtr Settings[sTyp], sTyp any](name, synopsis, usage string,
	exec ExecuteFunc[sPtr, sTyp], options ...Option,
) Command {
	settings, err := parseOptions(options...)
	if err != nil {
		panic(err)
	}
	cmd := &command{
		name:        name,
		synopsis:    synopsis,
		subcommands: settings.subcmds,
		niladic:     settings.niladic,
	}
	cmd.usage = makeUsage[sPtr](cmd, usage)
	cmd.execute = wrapExecute(cmd, settings.usageOutput, usage, exec)
	return cmd
}

func (cmd *command) Name() string           { return cmd.name }
func (cmd *command) Usage() string          { return cmd.usage() }
func (cmd *command) Synopsis() string       { return cmd.synopsis }
func (cmd *command) Subcommands() []Command { return cmd.subcommands }
func (cmd *command) Execute(ctx context.Context, args ...string) error {
	return cmd.execute(ctx, args...)
}

// We delay generating the result string,
// since [Command.Usage] is not likely to be called in the common case.
func makeUsage[sPtr Settings[sTyp], sTyp any](cmd *command, usage string) func() string {
	return func() string {
		var (
			name         = cmd.name
			flagSet      = flag.NewFlagSet(name, flag.ContinueOnError)
			flags   sPtr = new(sTyp)
		)
		flags.BindFlags(flagSet)
		helpText, err := formatHelpText(name, usage, flagSet, cmd.subcommands...)
		if err != nil {
			// Unlikely (but not impossible) I/O error.
			panic(err)
		}
		return helpText
	}
}

// formatHelpText constructs `-help` text
func formatHelpText(name, usage string,
	fs *flag.FlagSet, subcmds ...Command,
) (string, error) {
	sb := new(strings.Builder)
	sb.WriteString(
		"Usage: " + name + " [FLAGS] SUBCOMMAND\n\n" +
			usage + "\n\nFlags:\n",
	)
	buildFlagHelp(sb, fs)
	if len(subcmds) != 0 {
		sb.WriteString("\nSubcommands:\n")
		if err := buildSubcmdHelp(sb, subcmds...); err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}

// buildFlagHelp prints flagset help text into a string builder.
func buildFlagHelp(sb *strings.Builder, fs *flag.FlagSet) {
	defer fs.SetOutput(fs.Output())
	fs.SetOutput(sb)
	fs.PrintDefaults()
}

// buildSubcmdHelp creates list of subcommands formatted as 'name - synopsis`.
func buildSubcmdHelp(sb *strings.Builder, subs ...Command) error {
	tw := tabwriter.NewWriter(sb, 0, 0, 0, ' ', 0)
	for _, sub := range subs {
		if _, err := fmt.Fprintf(tw, "  %s\t - %s\n",
			sub.Name(), sub.Synopsis()); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	return nil
}

func printHelpText(output io.StringWriter, cmd *command, usage string, flagSet *flag.FlagSet) error {
	if output == nil {
		output = os.Stderr
	}
	helpText, err := formatHelpText(cmd.name, usage, flagSet, cmd.subcommands...)
	if err != nil {
		return err
	}
	if _, err := output.WriteString(helpText); err != nil {
		return err
	}
	return nil
}

func wrapExecute[sPtr Settings[sTyp], sTyp any,
](cmd *command, usageOutput io.StringWriter, usage string, exec ExecuteFunc[sPtr, sTyp],
) func(context.Context, ...string) error {
	return func(ctx context.Context, args ...string) error {
		flagSet := flag.NewFlagSet(cmd.name, flag.ContinueOnError)
		flags, arguments, err := parseArguments[sPtr](flagSet, args...)
		if err != nil {
			return err
		}
		var (
			helpRequested = flags.HelpRequested()
			haveArgs      = len(arguments) > 0
			tooManyArgs   = haveArgs && cmd.niladic
			usageError    = helpRequested || tooManyArgs
		)
		if !usageError {
			var (
				subcommands = cmd.subcommands
				haveSubs    = len(subcommands) > 0
			)
			if haveSubs && haveArgs {
				subname := arguments[0]
				for _, subcmd := range subcommands {
					if subcmd.Name() == subname {
						return subcmd.Execute(ctx, arguments[1:]...)
					}
				}
			}
			if err := exec(ctx, flags, arguments...); err == nil {
				return nil
			}
			if !errors.Is(err, ErrUsage) {
				return err
			}
			// fallthrough to print help
		}
		if err := printHelpText(usageOutput, cmd, usage, flagSet); err != nil {
			return err
		}
		return ErrUsage
	}
}

func parseArguments[sPtr Settings[sTyp], sTyp any](fs *flag.FlagSet,
	args ...string,
) (sPtr, []string, error) {
	var flags sPtr = new(sTyp)
	flags.BindFlags(fs)
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	return flags, fs.Args(), nil
}
