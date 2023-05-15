package commands

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/hugelgupf/p9/p9"
)

type (
	unmountSettings struct {
		clientSettings
		all bool
	}
	UnmountOption func(*unmountSettings) error
)

// TODO: shared option?
func UnmountAll(b bool) UnmountOption {
	return func(us *unmountSettings) error { us.all = b; return nil }
}

func (set *unmountSettings) BindFlags(flagSet *flag.FlagSet) {
	set.clientSettings.BindFlags(flagSet)
	flagSet.BoolVar(&set.all, "all", false, "unmount all")
}

// Unmount constructs the command which requests the file system service
// to undo the effects of a previous mount.
func Unmount() command.Command {
	const (
		name     = "unmount"
		synopsis = "Unmount file systems."
		usage    = "Placeholder text."
	)
	return command.MustMakeCommand[*unmountSettings](name, synopsis, usage, unmountExecute)
}

func unmountExecute(ctx context.Context, set *unmountSettings, args ...string) error {
	var (
		all      = set.all
		haveArgs = len(args) != 0
	)
	if all && haveArgs {
		return fmt.Errorf("%w - `all` flag cannot be combined with arguments", command.ErrUsage)
	}
	if !haveArgs && !all {
		return fmt.Errorf("%w - expected mount point(s)", command.ErrUsage)
	}

	const autoLaunchDaemon = false
	client, err := set.getClient(autoLaunchDaemon)
	if err != nil {
		return err
	}
	unmountOpts := []UnmountOption{
		UnmountAll(all),
	}
	return fserrors.Join(
		client.Unmount(ctx, args, unmountOpts...),
		client.Close(),
		ctx.Err(),
	)
}

func (c *Client) Unmount(ctx context.Context, targets []string, options ...UnmountOption) error {
	set := new(unmountSettings)
	for _, setter := range options {
		if err := setter(set); err != nil {
			return err
		}
	}
	mRoot, err := (*p9.Client)(c).Attach(mountsFileName)
	if err != nil {
		// TODO: if not-exist add context to err msg.
		// I.e. "client can't ... because ..."
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
