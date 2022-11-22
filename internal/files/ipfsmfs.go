package files

import (
	"context"
	"io/fs"
	"path"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/ipfs/go-cid"
	oldapi "github.com/ipfs/go-ipfs-api"
	"github.com/ipfs/go-mfs"
	"github.com/multiformats/go-multiaddr"
)

func getMFSMountRoot(ipfsAPIMultiaddr multiaddr.Multiaddr) (fs.FS, error) {
	oldShell, err := ipfsClient_old(ipfsAPIMultiaddr)
	if err != nil {
		return nil, err
	}
	// TODO: magic number; decide on good timeout and const it.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	entries, err := oldShell.FilesLs(ctx, "/", oldapi.FilesLs.Stat(true))
	if err != nil {
		return nil, err
	}
	const mountDirName = "mount" // TODO: make this configurable
	const mountDirPath = "/" + mountDirName
	var mountCidString string
	for _, ent := range entries {
		if ent.Name == mountDirName {
			mountCidString = ent.Hash
			break
		}
	}
	if mountCidString == "" {
		// TODO: [review] any options?
		if err := oldShell.FilesMkdir(ctx, mountDirPath); err != nil {
			return nil, err
		}
		stat, err := oldShell.FilesStat(ctx, mountDirPath)
		if err != nil {
			return nil, err
		}
		mountCidString = stat.Hash
	}
	mountCid, err := cid.Decode(mountCidString)
	if err != nil {
		return nil, err
	}
	newAPI, err := ipfsClient(ipfsAPIMultiaddr)
	if err != nil {
		return nil, err
	}
	publisher := mfsPublisher(mountDirName, oldShell)
	mroot, err := filesystem.CidToMFSRoot(context.TODO(), mountCid, newAPI, publisher)
	if err != nil {
		return nil, err
	}
	// TODO: no global context; port wart
	return filesystem.NewMFS(context.TODO(), mroot), nil
}

func mfsPublisher(mountDirName string, oldShell *oldapi.Shell) mfs.PubFunc {
	var (
		// TODO: we might want to nest our things deeper.
		// In case something goes wrong we don't want to pollute the MFS root.
		// e.g. MFS heir looks like /$mountDirRoot/$mountDirName
		// and staging happens within `/$mountDirRoot`
		// new CID -> /$mountDirRoot/$stageName -> /$mountDirRoot/$mountDirName
		stagePath = "/_stage_" + mountDirName // TODO: configurable
		tempPath  = "/_old_" + mountDirName   // TODO: configurable
		mountPath = "/" + mountDirName
	)
	return func(mfsCtx context.Context, mfsCid cid.Cid) error {
		// HACK: We would really like root access and the ability to just replace the root CID
		// This might be the best we can get until upstream allows this via the API somehow.
		// As close to atomic as we'll likely get, but each step could be interrupted/disconnected
		// so it's not really safe at all compared to a simple root-CID swap.
		// If the user mimicks these names, we're also boned. ☠️
		// TODO: condense. Quick hacks.
		if err := oldShell.FilesMkdir(mfsCtx, stagePath); err != nil {
			return err
		}
		canonicalPath := path.Join("/ipfs", mfsCid.String())
		if err := oldShell.FilesCp(mfsCtx, canonicalPath, stagePath); err != nil {
			return err
		}
		if err := oldShell.FilesMv(mfsCtx, mountPath, tempPath); err != nil {
			return err
		}
		if err := oldShell.FilesMv(mfsCtx, stagePath, mountPath); err != nil {
			return err
		}
		const force = true
		if err := oldShell.FilesRm(mfsCtx, tempPath, force); err != nil {
			return err
		}
		return nil
	}
}
