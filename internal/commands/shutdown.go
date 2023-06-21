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
			"\none of {%s}", shutdownLevelsText(),
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
	if err := applyOptions(&settings, so...); err != nil {
		return shutdownSettings{}, err
	}
	return settings, settings.clientSettings.fillDefaults()
}

func shutdownLevelsText() string {
	levels := make([]string, maximumShutdown)
	for i, sl := 0, minimumShutdown; sl <= maximumShutdown; i, sl = i+1, sl+1 {
		levels[i] = fmt.Sprintf(
			`"%s"`, strings.ToLower(sl.String()),
		)
	}
	return strings.Join(levels, ", ")
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
