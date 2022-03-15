package ipfs

import (
	"context"
	"path"
	"strings"

	"github.com/djdv/go-filesystem-utils/filesystem"
	ipld "github.com/ipfs/go-ipld-format" // TODO: migrate to new standard
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	corepath "github.com/ipfs/interface-go-ipfs-core/path"
)

// Bang, zoom, you're going to the moon.
func goToIPFSCore(fsid filesystem.ID, goPath string) corepath.Path {
	return corepath.New(
		path.Join("/",
			strings.ToLower(fsid.String()), // "ipfs", "ipns", ...
			goPath,
		),
	)
}

func resolveNode(ctx context.Context,
	core coreiface.CoreAPI, path corepath.Path) (ipld.Node, error) {
	ipldNode, err := core.ResolveNode(ctx, path)
	if err != nil {
		return nil, err
	}
	return ipldNode, nil
}
