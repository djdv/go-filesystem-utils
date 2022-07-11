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

func joinFlagAndSubcmds(name, usage string, fs *flag.FlagSet, subcmds ...Command) string {
	var (
		flagUsages       = formatFlags(fs)
		subcommandUsages = formatSubcommands(subcmds...)
		text             = new(strings.Builder)
	)
	fmt.Fprintf(text, "Usage of %s:", name)
	if flagUsages != "" {
		text.WriteString(" <flags>")
		flagUsages = fmt.Sprintf("Flags:\n%s\n", flagUsages)
	}
	if subcommandUsages != "" {
		// TODO: in [formatSubcommands] there is a commented out hack
		// It's not necessary but would be nice if we also printed out
		// `<subcommand arguments...>` if /any/ of the subcommands take arguments.
		// But not if none of them do.
		// This will likely require modifying the command interface, or something
		// so that we can inspect that here.
		text.WriteString(" <subcommand>")
		subcommandUsages = fmt.Sprintf("Subcommands:\n%s\n", subcommandUsages)
	}
	fmt.Fprintf(text, "\n\n%s\n\n", usage)
	fmt.Fprintf(text, "%s%s", flagUsages, subcommandUsages)
	return text.String()
}

func formatFlags(fs *flag.FlagSet) string {
	original := fs.Output()
	defer fs.SetOutput(original)

	textBuf := new(strings.Builder)
	fs.SetOutput(textBuf)
	fs.PrintDefaults()
	return textBuf.String()
}

func formatSubcommands(subcmds ...Command) string {
	var (
		textBuf   = new(strings.Builder)
		tabWriter = tabwriter.NewWriter(textBuf, 0, 0, 0, ' ', 0)
	)
	for _, subcmd := range subcmds {
		// HACK: should niladic be a method of the interface?
		// Command.ArgumentCount()? Then we can check for 0 or even more.
		// ^ I think a boolean is better than a count in this context.
		// The execute function should care about counts, not us (the formatter).
		//	if base, ok := subcmd.(*baseCmd); ok {
		//		if !base.niladic {
		//			sawArgs = true
		//		}
		//	}
		// NOTE: 2 leading spaces to match Go's default [flag] output.
		fmt.Fprintf(tabWriter, "  %s\t - %s\n", subcmd.Name(), subcmd.Synopsis())
	}
	if err := tabWriter.Flush(); err != nil {
		panic(err)
	}
	return textBuf.String()

	//if sawArgs {
	//	subcommandTag += " <subcommand args>"
	//}
}
