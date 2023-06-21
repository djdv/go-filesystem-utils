package commands

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/p9/p9"
)

type (
	unmountSettings struct {
		all bool
	}
	UnmountOption      func(*unmountSettings) error
	unmountCmdSettings struct {
		clientSettings
		apiOptions []UnmountOption
	}
	unmountCmdOption  func(*unmountCmdSettings) error
	unmountCmdOptions []unmountCmdOption
)

const (
	errUnmountMixed = generic.ConstError(`cannot combine "all" option with arguments`)
	errUnmountEmpty = generic.ConstError(`neither parameters nor "all" option was provided`)
)

func UnmountAll(b bool) UnmountOption {
	return func(us *unmountSettings) error {
		us.all = b
		return nil
	}
}

func (uo *unmountCmdOptions) BindFlags(flagSet *flag.FlagSet) {
	var clientOptions clientOptions
	(&clientOptions).BindFlags(flagSet)
	*uo = append(*uo, func(us *unmountCmdSettings) error {
		subset, err := clientOptions.make()
		if err != nil {
			return err
		}
		us.clientSettings = subset
		return nil
	})
	const (
		allName  = "all"
		allUsage = "unmount all"
	)
	flagSetFunc(flagSet, allName, allUsage, uo,
		func(value bool, settings *unmountCmdSettings) error {
			settings.apiOptions = append(settings.apiOptions, UnmountAll(value))
			return nil
		})
}

func (uo unmountCmdOptions) make() (unmountCmdSettings, error) {
	settings, err := makeWithOptions(uo...)
	if err != nil {
		return unmountCmdSettings{}, nil
	}
	return settings, settings.clientSettings.fillDefaults()
}

// Unmount constructs the command which requests the file system service
// to undo the effects of a previous mount.
func Unmount() command.Command {
	const (
		name     = "unmount"
		synopsis = "Unmount file systems."
	)
	usage := header("Unmount") +
		"\n\n" + synopsis +
		"\nAccepts mountpoints as arguments."
	return command.MakeVariadicCommand[unmountCmdOptions](name, synopsis, usage, unmountExecute)
}

func unmountExecute(ctx context.Context, arguments []string, options ...unmountCmdOption) error {
	settings, err := unmountCmdOptions(options).make()
	if err != nil {
		return err
	}
	const autoLaunchDaemon = false
	client, err := settings.getClient(autoLaunchDaemon)
	if err != nil {
		return err
	}
	apiOptions := settings.apiOptions
	if err := client.Unmount(ctx, arguments, apiOptions...); err != nil {
		if errors.Is(err, errUnmountEmpty) ||
			errors.Is(err, errUnmountMixed) {
			err = command.UsageError{Err: err}
		}
		return errors.Join(err, client.Close())
	}
	if err := client.Close(); err != nil {
		return err
	}
	return ctx.Err()
}

func (c *Client) Unmount(ctx context.Context, targets []string, options ...UnmountOption) error {
	settings, err := makeWithOptions(options...)
	if err != nil {
		return err
	}
	var (
		unmountAll  = settings.all
		haveTargets = len(targets) != 0
	)
	if unmountAll && haveTargets {
		return fmt.Errorf(
			"%w: %v",
			errUnmountMixed, targets,
		)
	}
	if !haveTargets && !unmountAll {
		return errUnmountEmpty
	}
	mounts, err := (*p9.Client)(c).Attach(mountsFileName)
	if err != nil {
		return err
	}
	if settings.all {
		if err := p9fs.UnmountAll(mounts); err != nil {
			err = receiveError(mounts, err)
			return errors.Join(err, mounts.Close())
		}
		return mounts.Close()
	}
	if err := p9fs.UnmountTargets(mounts, targets, decodeMountPoint); err != nil {
		err = receiveError(mounts, err)
		return errors.Join(err, mounts.Close())
	}
	return mounts.Close()
}

func decodeMountPoint(host filesystem.Host, _ filesystem.ID, data []byte) (string, error) {
	if host != cgofuse.HostID {
		return "", fmt.Errorf("unexpected host: %v", host)
	}
	// TODO: we should use `mountPointSettings`
	// same as [Mount], to assure consistency.
	// For now we only have 1 host type, so
	// ranging over them isn't necessary yet.
	var mountPoint struct {
		Host cgofuse.Host `json:"host"`
	}
	err := json.Unmarshal(data, &mountPoint)
	return mountPoint.Host.Point, err
}
