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
		idGen := c.idGen
		if idGen == nil {
			var err error
			if idGen, err = nanoid.CustomASCII(base58Alphabet, idLength); err != nil {
				return err
			}
			c.idGen = idGen
		}
		return handleFuse(mRoot, idGen, fsid, set, args)
	default:
		return errors.New("NIY")
	}
}

func handleFuse(mRoot p9.File, idGen nanoidGen, fsid filesystem.ID,
	set *mountSettings, targets []string,
) error {
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
	name := fmt.Sprintf("%s.json", idGen())
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
