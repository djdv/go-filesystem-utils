package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/p9/p9"
)

type (
	shutdownDisposition uint8
	shutdownSettings    struct {
		clientSettings
		disposition shutdownDisposition
	}
	shutdownOption  func(*shutdownSettings) error
	shutdownOptions []shutdownOption
)

const (
	dontShutdown shutdownDisposition = iota
	patientShutdown
	shortShutdown
	immediateShutdown
	minimumShutdown    = patientShutdown
	maximumShutdown    = immediateShutdown
	dispositionDefault = patientShutdown
)

func (level shutdownDisposition) String() string {
	switch level {
	case patientShutdown:
		return "patient"
	case shortShutdown:
		return "short"
	case immediateShutdown:
		return "immediate"
	default:
		return fmt.Sprintf("invalid: %d", level)
	}
}

// Shutdown constructs the command which
// requests the file system service to stop.
func Shutdown() command.Command {
	const (
		name     = "shutdown"
		synopsis = "Stop the system service."
	)
	usage := header("Shutdown") +
		"\n\nRequest to stop the file system services."
	return command.MakeVariadicCommand[shutdownOptions](name, synopsis, usage, shutdownExecute)
}

func (so *shutdownOptions) BindFlags(flagSet *flag.FlagSet) {
	var clientOptions clientOptions
	(&clientOptions).BindFlags(flagSet)
	*so = append(*so, func(ss *shutdownSettings) error {
		subset, err := clientOptions.make()
		if err != nil {
			return err
		}
		ss.clientSettings = subset
		return nil
	})
	const shutdownName = "level"
	shutdownUsage := fmt.Sprintf(
		"sets the `disposition` for shutdown"+
			"\none of:"+
			"\n%s",
		shutdownLevelsTable(),
	)
	flagSetFunc(flagSet, shutdownName, shutdownUsage, so,
		func(value shutdownDisposition, settings *shutdownSettings) error {
			settings.disposition = value
			return nil
		})
	flagSet.Lookup(shutdownName).
		DefValue = dispositionDefault.String()
}

func (so shutdownOptions) make() (shutdownSettings, error) {
	settings := shutdownSettings{
		disposition: dispositionDefault,
	}
	if err := generic.ApplyOptions(&settings, so...); err != nil {
		return shutdownSettings{}, err
	}
	return settings, nil
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
		level       shutdownDisposition
	}{
		{
			level:       patientShutdown,
			description: "waits for connections to become idle before closing",
		},
		{
			level:       shortShutdown,
			description: "forcibly closes connections after a short delay",
		},
		{
			level:       immediateShutdown,
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

func shutdownExecute(ctx context.Context, options ...shutdownOption) error {
	settings, err := shutdownOptions(options).make()
	if err != nil {
		return err
	}
	const autoLaunchDaemon = false
	client, err := settings.getClient(autoLaunchDaemon)
	if err != nil {
		return fmt.Errorf("could not get client (server already down?): %w", err)
	}
	if err := client.Shutdown(settings.disposition); err != nil {
		return errors.Join(err, client.Close())
	}
	if err := client.Close(); err != nil {
		return err
	}
	return ctx.Err()
}

func (c *Client) Shutdown(level shutdownDisposition) error {
	controlDir, err := (*p9.Client)(c).Attach(controlFileName)
	if err != nil {
		return err
	}
	_, shutdownFile, err := controlDir.Walk([]string{shutdownFileName})
	if err != nil {
		err = receiveError(controlDir, err)
		return errors.Join(err, controlDir.Close())
	}
	if _, _, err := shutdownFile.Open(p9.WriteOnly); err != nil {
		err = receiveError(controlDir, err)
		return errors.Join(err, shutdownFile.Close(), controlDir.Close())
	}
	data := []byte{byte(level)}
	if _, err := shutdownFile.WriteAt(data, 0); err != nil {
		err = receiveError(controlDir, err)
		return errors.Join(err, shutdownFile.Close(), controlDir.Close())
	}
	return errors.Join(shutdownFile.Close(), controlDir.Close())
}
