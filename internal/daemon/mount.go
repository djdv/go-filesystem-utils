package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/files"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/hugelgupf/p9/p9"
	"github.com/jaevor/go-nanoid"
	"github.com/multiformats/go-multiaddr"
)

type (
	// TODO: these should be shared literally
	// I.e. 9lib.Mount and client.Mount should use the same option type/structs
	MountOption   func(*mountSettings) error
	mountSettings struct {
		uid p9.UID
		gid p9.GID
		// fsid  filesystem.ID
		// fsapi filesystem.API
		/*
			fuse struct {
				uid, gid uint32
			}
		*/
		ipfs struct {
			nodeMaddr multiaddr.Multiaddr
		}
	}
)

// func (c *Client) Mount(args []string, options ...MountOptions) error {
func (c *Client) Mount(host filesystem.API, fsid filesystem.ID, args []string, options ...MountOption) error {
	set := new(mountSettings)
	for _, setter := range options {
		if err := setter(set); err != nil {
			return err
		}
	}
	switch host {
	case filesystem.Fuse:
		if len(args) == 0 {
			return fmt.Errorf("%w: no mountpoints provided", command.ErrUsage)
		}
		mRoot, err := c.p9Client.Attach(files.MounterName)
		if err != nil {
			return err
		}
		return handleFuse(mRoot, fsid, set, args)
	default:
		return errors.New("NIY")
	}
}

func handleFuse(mRoot p9.File, fsid filesystem.ID, set *mountSettings, targets []string) error {
	var (
		fuseName = strings.ToLower(filesystem.Fuse.String())
		fsidName = strings.ToLower(fsid.String())
		wname    = []string{fuseName, fsidName}
		uid      = set.uid
		gid      = set.gid
	)
	const permissions = files.S_IRWXA &^ (files.S_IWGRP | files.S_IWOTH)
	idRoot, err := files.MkdirAll(mRoot, wname, permissions, uid, gid)
	if err != nil {
		return err
	}

	// TODO: make target file, write opts, close.
	// ^ triggers mount on the server.

	// TODO: store this generator, don't remake it every time.
	newID9, err := nanoid.Standard(9)
	if err != nil {
		panic(err)
	}
	name := fmt.Sprintf("{%s}.json", newID9())
	targetFile, _, _, err := idRoot.Create(name, p9.ReadWrite, permissions, uid, gid)
	if err != nil {
		return err
	}

	data := struct {
		ApiMaddr multiaddr.Multiaddr
		Target   string
	}{
		Target: targets[0], // FIXME: args not handled
	}
	if serverMaddr := set.ipfs.nodeMaddr; serverMaddr != nil {
		data.ApiMaddr = serverMaddr
	}
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := targetFile.WriteAt(dataBytes, 0); err != nil {
		return err
	}
	return targetFile.Close()
}
