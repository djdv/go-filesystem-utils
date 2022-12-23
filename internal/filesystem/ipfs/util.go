package ipfs

import (
	"context"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	ipld "github.com/ipfs/go-ipld-format" // TODO: migrate to new standard
	dag "github.com/ipfs/go-merkledag"
	"github.com/ipfs/go-unixfs"
	unixpb "github.com/ipfs/go-unixfs/pb"
	corepath "github.com/ipfs/interface-go-ipfs-core/path"
)

type (
	unixFSInfo struct {
		name        string
		permissions fs.FileMode
		modtime     time.Time
		*unixfs.FSNode
	}
	ipldNodeInfo struct {
		name        string
		permissions fs.FileMode
		modtime     time.Time
		ipld.Node
	}
)

const rootName = "."

func readdir[ST any,
	translateFunc func(ST) filesystem.StreamDirEntry,
](ctx context.Context, source <-chan ST, translateFn translateFunc, count int,
) ([]fs.DirEntry, error) {
	const upperBound = 64
	var (
		entries   = make([]fs.DirEntry, 0, generic.Min(count, upperBound))
		returnAll = count <= 0
	)
	for {
		select {
		case sourceEntry, ok := <-source:
			if !ok {
				if len(entries) == 0 {
					return nil, io.EOF
				}
				return entries, nil
			}
			targetEnt := translateFn(sourceEntry)
			if err := targetEnt.Error(); err != nil {
				return entries, err
			}
			entries = append(entries, targetEnt)
			if !returnAll {
				if count--; count == 0 {
					return entries, nil
				}
			}
		case <-ctx.Done():
			return entries, ctx.Err()
		}
	}
}

func goToIPFSCore(fsid filesystem.ID, goPath string) (corepath.Path, error) {
	return corepath.New(
		path.Join("/",
			strings.ToLower(fsid.String()), // "ipfs", "ipns", ...
			goPath,
		)), nil
	/* TODO: This is only valid for IPFS. And likely isn't worth the fragility to save a resolve elsewhere.
	var (
		namespace    = strings.ToLower(fsid.String()) // "ipfs", "ipns", ...
		prefix       = path.Join("/", namespace)
		components   = strings.Split(goPath, "/")
		cidString    = components[0]
		rootCID, err = cid.Decode(cidString)
	)
	if err != nil {
		return nil, err
	}
	var (
		absoluteCID = path.Join(prefix, cidString)
		cidPath     = ipfspath.Path(absoluteCID)
		resolvedCID = corepath.NewResolvedPath(cidPath, rootCID, rootCID, "")
		remainder   = components[1:]
	)
	return corepath.Join(resolvedCID, remainder...), nil
	*/
}

func statNode(name string, modtime time.Time, permissions fs.FileMode,
	ipldNode ipld.Node,
) (fs.FileInfo, error) {
	if typedNode, ok := ipldNode.(*dag.ProtoNode); ok {
		ufsNode, err := unixfs.ExtractFSNode(typedNode)
		if err != nil {
			return nil, err
		}
		return &unixFSInfo{
			name:        name,
			permissions: permissions,
			modtime:     modtime,
			FSNode:      ufsNode,
		}, nil
	}
	//  *dag.RawNode, *cbor.Node
	return &ipldNodeInfo{
		name:        name,
		permissions: permissions,
		modtime:     modtime,
		Node:        ipldNode,
	}, nil
}

func (ufi *unixFSInfo) Name() string       { return ufi.name }
func (ufi *unixFSInfo) Size() int64        { return int64(ufi.FSNode.FileSize()) }
func (ufi *unixFSInfo) ModTime() time.Time { return ufi.modtime }
func (ufi *unixFSInfo) IsDir() bool        { return ufi.Mode().IsDir() }
func (ufi *unixFSInfo) Sys() any           { return ufi }
func (ufi *unixFSInfo) Mode() fs.FileMode {
	mode := ufi.permissions
	switch ufi.FSNode.Type() {
	case unixpb.Data_Directory, unixpb.Data_HAMTShard:
		mode |= fs.ModeDir
	case unixpb.Data_Symlink:
		mode |= fs.ModeSymlink
	case unixpb.Data_File, unixpb.Data_Raw:
	// NOOP:  mode |= fs.FileMode(0)
	default:
		mode |= fs.ModeIrregular
	}
	return mode
}

func (idi *ipldNodeInfo) Name() string { return idi.name }
func (idi *ipldNodeInfo) Size() int64 {
	nodeStat, err := idi.Node.Stat()
	if err != nil {
		return 0
	}
	return int64(nodeStat.CumulativeSize)
}
func (idi *ipldNodeInfo) Mode() fs.FileMode  { return idi.permissions }
func (idi *ipldNodeInfo) ModTime() time.Time { return idi.modtime }
func (idi *ipldNodeInfo) IsDir() bool        { return idi.Mode().IsDir() }
func (idi *ipldNodeInfo) Sys() any           { return idi }
