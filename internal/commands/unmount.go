package commands

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strconv"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/hugelgupf/p9/p9"
)

type (
	unmountCmdSettings struct {
		clientSettings
		options []UnmountOption
		allFlag bool
	}
	unmountSettings struct {
		all bool
	}
	UnmountOption func(*unmountSettings) error
)

func UnmountAll(b bool) UnmountOption {
	return func(us *unmountSettings) error {
		us.all = b
		return nil
	}
}

func (set *unmountCmdSettings) BindFlags(flagSet *flag.FlagSet) {
	set.clientSettings.BindFlags(flagSet)
	const (
		allName  = "all"
		allUsage = "unmount all"
	)
	boolFunc(flagSet, allName, allUsage, func(s string) error {
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		set.options = append(set.options, UnmountAll(b))
		set.allFlag = true
		return nil
	})
}

// Unmount constructs the command which requests the file system service
// to undo the effects of a previous mount.
func Unmount() command.Command {
	const (
		name     = "unmount"
		synopsis = "Unmount file systems."
		usage    = "Placeholder text."
	)
	return command.MustMakeCommand[*unmountCmdSettings](name, synopsis, usage, unmountExecute)
}

func unmountExecute(ctx context.Context, set *unmountCmdSettings, args ...string) error {
	var (
		allFlag  = set.allFlag
		haveArgs = len(args) != 0
	)
	if allFlag && haveArgs {
		return fmt.Errorf(
			"%w - `all` flag cannot be combined with arguments",
			command.ErrUsage,
		)
	}
	if !haveArgs && !allFlag {
		return fmt.Errorf(
			"%w - expected mount point(s)",
			command.ErrUsage,
		)
	}
	const autoLaunchDaemon = false
	client, err := set.getClient(autoLaunchDaemon)
	if err != nil {
		return err
	}
	options := set.options
	return fserrors.Join(
		client.Unmount(ctx, args, options...),
		client.Close(),
		ctx.Err(),
	)
}

func (c *Client) Unmount(ctx context.Context, targets []string, options ...UnmountOption) error {
	var set unmountSettings
	for _, setter := range options {
		if err := setter(&set); err != nil {
			return err
		}
	}
	mRoot, err := (*p9.Client)(c).Attach(mountsFileName)
	if err != nil {
		return err
	}
	if set.all {
		if err := p9fs.UnmountAll(mRoot); err != nil {
			return err
		}
		return ctx.Err()
	}
	if err := p9fs.UnmountTargets(mRoot, targets, decodeMountPoint); err != nil {
		return err
	}
	return ctx.Err()
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
