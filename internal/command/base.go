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

type (
	// TODO: docs
	// Can/must be embedded into any command flag structure.
	// Automatically implements a check that looks for `--help`
	// and returns, before even calling the user's provided execute function
	// for a command.
	HelpFlag bool

	// TODO: docs
	// Settings is a constraint that permits any reference type
	// which also implements a [FlagBinder] and the help flag hook method.
	Settings[settings any] interface {
		*settings
		FlagBinder
		NeedsHelp() bool // TODO: better name? Break out into distinct interface?
	}

	// TODO: name and docs
	// the primary expected signature of a command's Execute function/method.
	ExecuteFunc[settings Settings[sTyp],
		sTyp any] func(context.Context, settings, ...string) error

	// TODO: name and docs
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
// We have to use dynamic dispatch instead. See use of [NeedsHelp] method.

// TODO: name + docs
// This binds the HelpFlag type to a flagset.
// Currently this is just a bool, but could in theory change later.
// (E.g. instead of just "help" we can distinguish short `-h` / long `--help` etc. )
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

func (b HelpFlag) NeedsHelp() bool { return bool(b) }

func (b *HelpFlag) String() string { return strconv.FormatBool(bool(*b)) }

func (b *HelpFlag) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	*b = HelpFlag(v)
	return nil
}

func MakeCommand[sPtr Settings[sTyp], sTyp any](name, synopsis, usage string,
	exec ExecuteFunc[sPtr, sTyp], options ...Option,
) Command {
	settings, err := parseOptions(options...)
	if err != nil {
		panic(err)
	}
	var (
		subcommands = settings.subcmds
		niladic     = settings.niladic
		output      = settings.usageOutput

		formatUsage = func(flagSet *flag.FlagSet) string {
			return joinFlagAndSubcmds(name, usage, flagSet, subcommands...)
		}
		printUsage = func(flagSet *flag.FlagSet) {
			if output == nil {
				output = os.Stderr
			}
			output.WriteString(formatUsage(flagSet))
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
			return formatUsage(flagSet)
		},
		subcommands: subcommands,
		niladic:     niladic,
		execute: func(ctx context.Context, args ...string) error {
			flagSet := flag.NewFlagSet(name, flag.ContinueOnError)
			flags, arguments, err := parseArguments[sPtr](flagSet, args...)
			if err != nil {
				return err
			}

			if flags.NeedsHelp() {
				printUsage(flagSet)
				return ErrUsage
			}

			if len(arguments) == 0 {
				if err := exec(ctx, flags); err != nil {
					if errors.Is(err, ErrUsage) {
						printUsage(flagSet)
					}
					return err
				}
				return nil
			}
			if niladic {
				printUsage(flagSet)
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
						printUsage(flagSet)
					}
					return err
				}
				return nil
			}

			printUsage(flagSet)
			return ErrUsage // Subcommand not found.
			// TODO: ^ we could repeat the input here, maybe we should.
			// E.g. `prog.exe unexpected` would print something like
			// "unexpected" is not a subcommand; or whatever.
		},
	}
}

func (base *command) Name() string           { return base.name }
func (base *command) Synopsis() string       { return base.synopsis }
func (base *command) Subcommands() []Command { return base.subcommands }
func (base *command) Execute(ctx context.Context, args ...string) error {
	return base.execute(ctx, args...)
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

func (base *command) Usage() string {
	return base.usage()
}

func joinFlagAndSubcmds(name, usage string,
	fs *flag.FlagSet, subcmds ...Command) string {
	var (
		sb            = new(strings.Builder)
		buildFlagHelp = func(sb *strings.Builder, fs *flag.FlagSet) {
			defer fs.SetOutput(fs.Output())
			fs.SetOutput(sb)
			fs.PrintDefaults()
		}
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
		"Usage: " + name + " [FLAGS] SUBCOMMAND" +
			"\n\nFlags:\n",
	)
	buildFlagHelp(sb, fs)
	sb.WriteString("\nSubcommands:\n")
	buildSubcmdHelp(sb, subcmds...)
	return sb.String()
}
