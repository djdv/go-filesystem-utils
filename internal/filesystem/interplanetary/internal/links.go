package interplanetary

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	coreiface "github.com/ipfs/boxo/coreiface"
	corepath "github.com/ipfs/boxo/coreiface/path"
	"github.com/ipfs/boxo/files"
	mdag "github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs"
	"github.com/ipfs/go-cid"
)

func GetUnixFSLink(ctx context.Context,
	op, name string,
	ufs coreiface.UnixfsAPI, cid cid.Cid,
	allowedPrefix string,
) (string, error) {
	cPath := corepath.IpfsPath(cid)
	link, err := ufs.Get(ctx, cPath)
	if err != nil {
		const kind = fserrors.IO
		return "", fserrors.New(op, name, err, kind)
	}
	return resolveNodeLink(op, name, link, allowedPrefix)
}

func resolveNodeLink(op, name string, node files.Node, prefix string) (string, error) {
	target, err := readNodeLink(op, name, node)
	if err != nil {
		return "", err
	}
	// We allow 2 kinds of absolute links:
	// 1) File system's root
	// 2) Paths matching an explicitly allowed prefix
	if strings.HasPrefix(target, prefix) {
		target = strings.TrimPrefix(target, prefix)
		return path.Clean(target), nil
	}
	switch target {
	case "/":
		return filesystem.Root, nil
	case "..":
		name = path.Dir(name)
		fallthrough
	case ".":
		return path.Dir(name), nil
	}
	if target[0] == '/' {
		const (
			err  = generic.ConstError("link target must be relative")
			kind = fserrors.InvalidItem
		)
		pair := fmt.Sprintf(
			`%s -> %s`,
			name, target,
		)
		return "", fserrors.New(op, pair, err, kind)
	}
	if target = path.Join("/"+name, target); target == "/" {
		target = filesystem.Root
	}
	return target, nil
}

func readNodeLink(op, name string, node files.Node) (string, error) {
	link, ok := node.(*files.Symlink)
	if !ok {
		const kind = fserrors.InvalidItem
		err := fmt.Errorf(
			"expected node type: %T but got: %T",
			link, node,
		)
		return "", fserrors.New(op, name, err, kind)
	}
	target := link.Target
	if len(target) == 0 {
		const kind = fserrors.InvalidItem
		return "", fserrors.New(op, name, ErrEmptyLink, kind)
	}
	return target, nil
}

func MakeAndAddLink(ctx context.Context, target string, dag coreiface.APIDagService) (cid.Cid, error) {
	dagData, err := unixfs.SymlinkData(target)
	if err != nil {
		return cid.Cid{}, err
	}
	dagNode := mdag.NodeWithData(dagData)
	if err := dag.Add(ctx, dagNode); err != nil {
		return cid.Cid{}, err
	}
	return dagNode.Cid(), nil
}
