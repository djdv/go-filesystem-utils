package commands

import (
	"context"
	"flag"
	"fmt"

	"github.com/djdv/go-filesystem-utils/internal/command"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
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
	return command.MakeCommand[*unmountSettings](name, synopsis, usage, unmountExecute)
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

	const launch = false
	client, err := getClient(&set.clientSettings, launch)
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
	mRoot, err := c.p9Client.Attach(p9fs.MounterName)
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
	if err := p9fs.UnmountTargets(mRoot, targets); err != nil {
		return err
	}
	return ctx.Err()
}
