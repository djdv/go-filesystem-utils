package commands

import (
	"flag"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/commands/daemon"
	"github.com/djdv/go-filesystem-utils/internal/commands/shutdown"
)

type shutdownOptions []shutdown.Option

// Shutdown constructs the [command.Command]
// which requests the file system service to stop.
func Shutdown() command.Command {
	const (
		name     = "shutdown"
		synopsis = "Stop the system service."
	)
	usage := heading("Shutdown") +
		"\n\nRequest to stop the file system services."
	return command.MakeVariadicCommand[shutdownOptions](
		name, synopsis, usage,
		shutdown.Request,
	)
}

func (so *shutdownOptions) BindFlags(flagSet *flag.FlagSet) {
	so.bindClientFlags(flagSet)
	so.bindLevelFlag(flagSet)
}

func (so *shutdownOptions) bindClientFlags(flagSet *flag.FlagSet) {
	inheritClientFlags(
		flagSet, so, nil, // No default client options.
		shutdown.WithClientOptions,
	)
}

func (so *shutdownOptions) bindLevelFlag(flagSet *flag.FlagSet) {
	const name = "level"
	var (
		usage = fmt.Sprintf(
			"sets the `disposition` for shutdown"+
				"\none of:"+
				"\n%s",
			shutdownLevelsTable(),
		)
		transformFn = func(level daemon.ShutdownDisposition) shutdown.Option {
			return shutdown.WithDisposition(level)
		}
	)
	insertSliceOnce(
		flagSet, name, usage,
		so, daemon.ParseShutdownLevel, transformFn,
	)
	flagSet.Lookup(name).
		DefValue = shutdown.DefaultDisposition.String()
}

func shutdownLevelsTable() string {
	// [upstream] glamour prepends a newline to lists
	// which can not be disabled. So we don't use them here. :^/
	const (
		minWidth = 0
		tabWidth = 0
		padding  = 0
		padChar  = ' '
		flags    = 0
	)
	var (
		levelsBuffer strings.Builder
		tabWriter    = tabwriter.NewWriter(
			&levelsBuffer, minWidth, tabWidth, padding, padChar, flags,
		)
	)
	for _, pair := range []struct {
		description string
		level       daemon.ShutdownDisposition
	}{
		{
			level:       daemon.ShutdownPatient,
			description: "waits for connections to become idle before closing",
		},
		{
			level:       daemon.ShutdownShort,
			description: "forcibly closes connections after a short delay",
		},
		{
			level:       daemon.ShutdownImmediate,
			description: "forcibly closes connections immediately",
		},
	} {
		if _, err := fmt.Fprintf(
			tabWriter,
			"`%s`\t - %s\n",
			pair.level, pair.description,
		); err != nil {
			panic(err)
		}
	}
	if err := tabWriter.Flush(); err != nil {
		panic(err)
	}
	return levelsBuffer.String()
}
