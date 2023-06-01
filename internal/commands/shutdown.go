package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/p9/p9"
)

type (
	shutdownDisposition uint8
	shutdownSettings    struct {
		clientSettings
		disposition shutdownDisposition
	}
)

const (
	dontShutdown shutdownDisposition = iota
	patientShutdown
	shortShutdown
	immediateShutdown
	minimumShutdown = patientShutdown
	maximumShutdown = immediateShutdown
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
		usage    = "Request to stop the file system service."
	)
	return command.MustMakeCommand[*shutdownSettings](name, synopsis, usage, shutdownExecute)
}

func (set *shutdownSettings) BindFlags(flagSet *flag.FlagSet) {
	set.clientSettings.BindFlags(flagSet)
	const shutdownName = "level"
	shutdownValues := make([]string, maximumShutdown)
	for i, sl := 0, minimumShutdown; sl <= maximumShutdown; i, sl = i+1, sl+1 {
		shutdownValues[i] = fmt.Sprintf(`"%s"`,
			strings.ToLower(sl.String()),
		)
	}
	shutdownUsage := fmt.Sprintf(
		"sets the `disposition` for shutdown\none of {%s}",
		strings.Join(shutdownValues, ", "),
	)
	set.disposition = patientShutdown
	shutdownDefaultText := patientShutdown.String()
	flagSet.Func(shutdownName, shutdownUsage, func(s string) (err error) {
		set.disposition, err = parseShutdownLevel(s)
		return
	})
	setDefaultValueText(flagSet, flagDefaultText{
		shutdownName: shutdownDefaultText,
	})
}

func shutdownExecute(ctx context.Context, set *shutdownSettings) error {
	const autoLaunchDaemon = false
	client, err := set.getClient(autoLaunchDaemon)
	if err != nil {
		return fmt.Errorf("could not get client (server already down?): %w", err)
	}
	if err := client.Shutdown(set.disposition); err != nil {
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
