package daemon

import (
	"context"
	"errors"

	"github.com/djdv/go-filesystem-utils/internal/files"
	"github.com/hugelgupf/p9/p9"
)

// NOTE: not stable, for development/basic use only right now.
// Use 9P API directly for fine-control in the meantime.
func (c *Client) Unmount(ctx context.Context, options ...UnmountOption) error {
	set := new(unmountSettings)
	for _, setter := range options {
		if err := setter(set); err != nil {
			return err
		}
	}
	all := set.all
	if !all {
		return errors.New("single targets not implemented yet, use `-a`")
	}
	mRoot, err := c.p9Client.Attach(files.MounterName)
	if err != nil {
		// TODO: if not-exist fail softer.
		return err
	}
	if err := removeMounts(mRoot); err != nil {
		return err
	}
	return ctx.Err()
}

func removeMounts(fsys p9.File) error {
	ents, err := files.ReadDir(fsys)
	if err != nil {
		return err
	}
	for _, ent := range ents {
		switch ent.Type {
		case p9.TypeRegular:
			if err := fsys.UnlinkAt(ent.Name, 0); err != nil {
				// TODO: continue on err?
				return err
			}
		case p9.TypeDir:
			_, sub, err := fsys.Walk([]string{ent.Name})
			if err != nil {
				return err
			}
			if err := removeMounts(sub); err != nil {
				return err
			}
		}
	}
	return nil
}
