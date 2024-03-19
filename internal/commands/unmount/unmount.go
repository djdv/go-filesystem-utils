package unmount

import (
	"context"
	"errors"
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/commands/client"
	"github.com/djdv/go-filesystem-utils/internal/commands/daemon"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/p9/p9"
)

type (
	settings struct {
		client client.Options
		all    bool
	}
	Option     func(*settings) error
	decodeFunc func(data []byte) (string, error)
	decoders   map[filesystem.Host]decodeFunc
)

const (
	ErrUnmountMixed = generic.ConstError(`cannot combine "all" option with arguments`)
	ErrUnmountEmpty = generic.ConstError(`neither parameters nor "all" option was provided`)
)

// Detach undoes the effects of a previous call to
// [mount.Attach].
func Detach(ctx context.Context, targets []string, options ...Option) error {
	var settings settings
	if err := generic.ApplyOptions(&settings, options...); err != nil {
		return err
	}
	var (
		all         = settings.all
		haveTargets = len(targets) != 0
	)
	if all && haveTargets {
		return fmt.Errorf(
			"%w: %v",
			ErrUnmountMixed, targets,
		)
	}
	if !haveTargets && !all {
		return ErrUnmountEmpty
	}
	client, err := settings.client.GetClient()
	if err != nil {
		return err
	}
	return errors.Join(
		communicate(client, targets, all),
		client.Close(),
		ctx.Err(),
	)
}

func communicate(client *p9.Client, targets []string, all bool) error {
	root, err := client.Attach("")
	if err != nil {
		return err
	}
	_, mounts, err := root.Walk([]string{daemon.MountsFileName})
	if err != nil {
		return generic.CloseWithError(
			daemon.ReceiveError(root, err),
			root,
		)
	}
	if all {
		err = p9fs.UnmountAll(mounts)
	} else {
		decodeFn := newDecodeTargetFunc()
		err = p9fs.UnmountTargets(mounts, targets, decodeFn)
	}
	if err != nil {
		err = daemon.ReceiveError(mounts, err)
	}
	return generic.CloseWithError(
		err,
		mounts, root,
	)
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

func All(b bool) Option {
	const name = "All"
	return func(settings *settings) error {
		if settings.all {
			return generic.OptionAlreadySet(name)
		}
		settings.all = b
		return nil
	}
}

func newDecodeTargetFunc() p9fs.DecodeTargetFunc {
	type makeDecoderFunc func() (filesystem.Host, decodeFunc)
	var (
		decoderMakers = []makeDecoderFunc{
			unmarshalFUSE,
			// unmarshalNFS,
		}
		decoders = make(decoders, len(decoderMakers))
	)
	for _, decoderMaker := range decoderMakers {
		host, decoder := decoderMaker()
		if decoder == nil {
			continue // System (likely) disabled by build constraints.
		}
		decoders[host] = decoder
	}
	return func(data []byte) (string, error) {
		tag, hostData, _, err := mountpoint.SplitData(data)
		if err != nil {
			return "", nil
		}
		var (
			hostID      = tag.Host
			decoder, ok = decoders[hostID]
		)
		if !ok {
			return "", fmt.Errorf("unexpected host: %v", hostID)
		}
		return decoder(hostData)
	}
}
