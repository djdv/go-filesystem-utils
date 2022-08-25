package command

import (
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
)

type StringWriter interface {
	io.Writer
	io.StringWriter
}

// printHelpText formats `-help` text.
func printHelpText(output StringWriter,
	name, usage string,
	flagSet *flag.FlagSet, subcommands ...Command,
) error {
	var (
		haveFlags bool
		haveSubs  = len(subcommands) > 0
	)
	flagSet.VisitAll(func(*flag.Flag) { haveFlags = true })
	if err := printUsage(output, name, usage, haveFlags, haveSubs); err != nil {
		return err
	}
	if err := printFlagHelp(output, flagSet); err != nil {
		return err
	}
	if haveSubs {
		return printSubcommandHelp(output, subcommands...)
	}
	return nil
}

// printUsage formats the command's usage string.
// i.e. Usage: name [FLAGS] | Usage: name [FLAG] SUBCOMMAND
func printUsage(output io.StringWriter,
	name, usage string, haveFlags, haveSubs bool,
) error {
	if _, err := output.WriteString("Usage: " + name); err != nil {
		return err
	}
	if haveFlags {
		if _, err := output.WriteString(" [FLAGS]"); err != nil {
			return err
		}
	}
	if haveSubs {
		if _, err := output.WriteString(" SUBCOMMAND"); err != nil {
			return err
		}
	}
	if _, err := output.WriteString("\n\n" + usage + "\n\n"); err != nil {
		return err
	}
	return nil
}

// printFlagHelp formats [FlagSet].
func printFlagHelp(output StringWriter, flagSet *flag.FlagSet) error {
	defer flagSet.SetOutput(flagSet.Output())
	if _, err := output.WriteString("Flags:\n"); err != nil {
		return err
	}
	flagSet.SetOutput(output)
	flagSet.PrintDefaults()
	if _, err := output.WriteString("\n"); err != nil {
		return err
	}
	return nil
}

// printSubcommandHelp creates list of subcommands formatted as 'name - synopsis`.
func printSubcommandHelp(output StringWriter, subs ...Command) error {
	if _, err := output.WriteString("Subcommands:\n"); err != nil {
		return err
	}
	var (
		tabWriter = tabwriter.NewWriter(output, 0, 0, 0, ' ', 0)
		subTail   = len(subs) - 1
	)

	for i, sub := range subs {
		if _, err := fmt.Fprintf(tabWriter,
			"  %s\t - %s\n",
			sub.Name(), sub.Synopsis(),
		); err != nil {
			return err
		}
		if i == subTail {
			fmt.Fprintln(tabWriter)
		}
	}
	if err := tabWriter.Flush(); err != nil {
		return err
	}
	return nil
}
