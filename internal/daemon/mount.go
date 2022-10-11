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
	"github.com/hugelgupf/p9/perrors"
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
		if err := handleFuse(mRoot, idGen, fsid, set, args); err != nil {
			if errors.Is(err, perrors.EIO) {
				/* TODO: Unfortunately the .L variant of 9P
				uses numbers instead of strings for errors;
				so we lose any additional information.
				For now we'll ambiguously decorate the error.
				Later we can inspect args to be more precise
				(this one only applies to IPFS targets)
				We can also consider setting up an extension on the daemon,
				which lets us inquire deeper.
				E.g. before the call, request a token,
				if we get an error, send both back to the daemon.
				Daemon then responds with the original error string.
				This allows us to remain compliant with .L clients
				without compromising on clarity in our specific client.
				*/
				return fmt.Errorf("%w: %s", err, "IPFS node may be unreachable?")
			}
			return err
		}
		return nil
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
	const permissions = files.S_IRWXU | files.S_IRA | files.S_IXA
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
