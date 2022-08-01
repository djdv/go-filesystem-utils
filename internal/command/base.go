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
	exec ExecuteFunc[sPtr, sTyp], options ...Option) Command {
	settings, err := parseOptions(options...)
	if err != nil {
		panic(err)
	}
	var (
		subcommands = settings.subcmds
		niladic     = settings.niladic
		output      = settings.usageOutput

		formatHelpText = func(flagSet *flag.FlagSet) string {
			return joinFlagAndSubcmds(name, usage, flagSet, subcommands...)
		}
		printHelpText = func(flagSet *flag.FlagSet) {
			if output == nil {
				output = os.Stderr
			}
			output.WriteString(formatHelpText(flagSet))
		}
	)

	return &command{
		name:     name,
		synopsis: synopsis,
		usage: func() string {
			var (
				flagSet      = flag.NewFlagSet(name, flag.ContinueOnError)
				flags   sPtr = new(sTyp)
			)
			flags.BindFlags(flagSet)
			return formatHelpText(flagSet)
		},
		subcommands: subcommands,
		niladic:     niladic,
		execute: func(ctx context.Context, args ...string) error {
			flagSet := flag.NewFlagSet(name, flag.ContinueOnError)
			flags, arguments, err := parseArguments[sPtr](flagSet, args...)
			if err != nil {
				return err
			}

			if flags.HelpRequested() {
				printHelpText(flagSet)
				return ErrUsage
			}

			if len(arguments) == 0 {
				if err := exec(ctx, flags); err != nil {
					if errors.Is(err, ErrUsage) {
						printHelpText(flagSet)
					}
					return err
				}
				return nil
			}
			if niladic {
				printHelpText(flagSet)
				return ErrUsage // Arguments provided but command takes none.
			}

			subname := arguments[0]
			for _, subcmd := range subcommands {
				if subcmd.Name() == subname {
					return subcmd.Execute(ctx, arguments[1:]...)
				}
			}

			// HACK: we need a proper solution for this
			// invoke exec when command has subcommands but not found
			// otherwise return usage? idk
			// Also repetition with head of func.
			if len(subcommands) == 0 {
				if err := exec(ctx, flags, arguments...); err != nil {
					if errors.Is(err, ErrUsage) {
						printHelpText(flagSet)
					}
					return err
				}
				return nil
			}

			printHelpText(flagSet)
			return ErrUsage // Subcommand not found.
			// TODO: ^ we could repeat the input here, maybe we should.
			// E.g. `prog.exe unexpected` would print something like
			// "unexpected" is not a subcommand; or whatever.
		},
	}
}

func (cmd *command) Name() string           { return cmd.name }
func (cmd *command) Usage() string          { return cmd.usage() }
func (cmd *command) Synopsis() string       { return cmd.synopsis }
func (cmd *command) Subcommands() []Command { return cmd.subcommands }
func (cmd *command) Execute(ctx context.Context, args ...string) error {
	return cmd.execute(ctx, args...)
}

func printIfUsageErr(output io.StringWriter, err error, usage string) error {
	if errors.Is(err, ErrUsage) {
		output.WriteString(usage)
	}
	return err
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

func handleExecErr(cmd Command, err error) error {
	if errors.Is(err, ErrUsage) {
		fmt.Fprint(os.Stderr, cmd.Usage())
	}
	return err
}

// Constructs -help text
func joinFlagAndSubcmds(name, usage string,
	fs *flag.FlagSet, subcmds ...Command) string {
	var (
		sb = new(strings.Builder)
		// prints flagset help text into a string builder
		buildFlagHelp = func(sb *strings.Builder, fs *flag.FlagSet) {
			defer fs.SetOutput(fs.Output())
			fs.SetOutput(sb)
			fs.PrintDefaults()
		}
		// creates list of subcommands formatted as 'name - synopsis`
		buildSubcmdHelp = func(sb *strings.Builder, subs ...Command) {
			tw := tabwriter.NewWriter(sb, 0, 0, 0, ' ', 0)
			for _, sub := range subs {
				fmt.Fprintf(tw, "  %s\t - %s\n",
					sub.Name(), sub.Synopsis())
			}
			if err := tw.Flush(); err != nil {
				panic(err)
			}
		}
	)
	sb.WriteString(
		"Usage: " + name + " [FLAGS] SUBCOMMAND\n\n" +
			usage + "\n\nFlags:\n",
	)
	buildFlagHelp(sb, fs)
	// bw TODO: this feels wrong, maybe subcmd stuff should be a new func
	if len(subcmds) != 0 {
		sb.WriteString("\nSubcommands:\n")
		buildSubcmdHelp(sb, subcmds...)
	}
	return sb.String()
}
