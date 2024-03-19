package shutdown

import (
	"context"
	"errors"
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/commands/client"
	"github.com/djdv/go-filesystem-utils/internal/commands/daemon"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/p9/p9"
)

type (
	settings struct {
		client      client.Options
		disposition daemon.ShutdownDisposition
	}
	Option func(*settings) error
)

const DefaultDisposition = daemon.ShutdownPatient

// Request the file system server to shut down.
func Request(ctx context.Context, options ...Option) error {
	settings := settings{
		disposition: DefaultDisposition,
	}
	if err := generic.ApplyOptions(&settings, options...); err != nil {
		return err
	}
	client, err := settings.client.GetClient()
	if err != nil {
		return fmt.Errorf("could not get client (server already down?): %w", err)
	}
	return errors.Join(
		communicate(client, settings.disposition),
		client.Close(),
		ctx.Err(),
	)
}

func communicate(client *p9.Client, level daemon.ShutdownDisposition) error {
	root, err := client.Attach("")
	if err != nil {
		return err
	}
	_, controlDir, err := root.Walk([]string{daemon.ControlFileName})
	if err != nil {
		return generic.CloseWithError(
			daemon.ReceiveError(root, err),
			root,
		)
	}
	_, shutdownFile, err := controlDir.Walk([]string{daemon.ShutdownFileName})
	if err != nil {
		return generic.CloseWithError(
			daemon.ReceiveError(root, err),
			controlDir, root,
		)
	}
	if _, _, err := shutdownFile.Open(p9.WriteOnly); err != nil {
		return generic.CloseWithError(
			daemon.ReceiveError(root, err),
			shutdownFile, controlDir, root,
		)
	}
	data := []byte{byte(level)}
	if _, err = shutdownFile.WriteAt(data, 0); err != nil {
		err = daemon.ReceiveError(root, err)
	}
	return generic.CloseWithError(
		err,
		shutdownFile, controlDir, root,
	)
}

func WithDisposition(level daemon.ShutdownDisposition) Option {
	return func(settings *settings) error {
		settings.disposition = level
		return nil
	}
}

func WithClientOptions(options ...client.Option) Option {
	const name = "WithClientOptions"
	return func(settings *settings) error {
		if settings.client != nil {
			return generic.OptionAlreadySet(name)
		}
		settings.client = options
		return nil
	}
}
