package filesystem

import (
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	ipld "github.com/ipfs/go-ipld-format" // TODO: migrate to new standard
	dag "github.com/ipfs/go-merkledag"
	gopath "github.com/ipfs/go-path"
	"github.com/ipfs/go-unixfs"
	unixpb "github.com/ipfs/go-unixfs/pb"
	corepath "github.com/ipfs/interface-go-ipfs-core/path"
)

func goToIPFSCore(fsid ID, goPath string) corepath.Path {
	dbgErrPath := func(err error) corepath.Path {
		// FIXME: sig needs to return errs
		// TODO: debug path value; we should return this err instead.
		return corepath.New(
			path.Join("/",
				goPath, err.Error(),
			))
	}
	var (
		pathComponents = strings.Split(goPath, "/")
		cidStr         = pathComponents[0]
	)
	rootCID, err := cid.Decode(cidStr)
	if err != nil {
		return dbgErrPath(err)
	}

	pathPrefix := path.Join("/",
		strings.ToLower(fsid.String()), // "ipfs", "ipns", ...
	)

	if rootCID.Version() >= 1 {
		return corepath.New(path.Join(pathPrefix, goPath))
	}

	rootCID = upgradeCid(rootCID)
	return corepath.Join(corepath.NewResolvedPath(
		gopath.Path(path.Join(
			pathPrefix,
			rootCID.String(),
		)),
		rootCID, rootCID, ""),
		pathComponents[1:]...,
	)

	/*
		newRoot := gopath.Path(path.Join(
			"/",
			strings.ToLower(fsid.String()), // "ipfs", "ipns", ...
			rootCID.String(),
		))
		return corepath.Join(newRoot, pathComponents[1:]...)
	*/
	/*
		return corepath.New(
			path.Join("/",
				strings.ToLower(fsid.String()), // "ipfs", "ipns", ...
				goPath,
			),
		)
	*/
}

func upgradeCid(c cid.Cid) cid.Cid { return cid.NewCidV1(c.Type(), c.Hash()) }

func statNode(name string, modtime time.Time, permissions fs.FileMode,
	ipldNode ipld.Node,
) (fs.FileInfo, error) {
	if typedNode, ok := ipldNode.(*dag.ProtoNode); ok {
		ufsNode, err := unixfs.ExtractFSNode(typedNode)
		if err != nil {
			return nil, err
		}
		return unixFSAttr(name, modtime, permissions, ufsNode)
	}
	//  *dag.RawNode, *cbor.Node
	return genericAttr(name, modtime, permissions, ipldNode)
}

func genericAttr(name string, modtime time.Time, permissions fs.FileMode,
	genericNode ipld.Node,
) (fs.FileInfo, error) {
	// raw nodes only contain data so we'll treat them as a flat file
	// cbor nodes are not currently supported via UnixFS so we assume them to contain only data
	// TODO: review ^ is there some way we can implement this that won't blow up in the future?
	// (if unixfs supports cbor and directories are implemented to use them )
	nodeStat, err := genericNode.Stat()
	if err != nil {
		return nil, err
	}
	return staticStat{
		size:    int64(nodeStat.CumulativeSize),
		name:    name,
		mode:    permissions,
		modTime: modtime,
	}, nil
}

func unixFSAttr(name string, modtime time.Time, permissions fs.FileMode,
	ufsNode *unixfs.FSNode,
) (fs.FileInfo, error) {
	return staticStat{
		name:    name,
		size:    int64(ufsNode.FileSize()),
		mode:    unixfsTypeToGoType(ufsNode.Type()) | permissions,
		modTime: modtime, // TODO: from UFS when v2 lands.
	}, nil
}

func unixfsTypeToGoType(ut unixpb.Data_DataType) fs.FileMode {
	switch ut {
	case unixpb.Data_Directory, unixpb.Data_HAMTShard:
		return fs.ModeDir
	case unixpb.Data_Symlink:
		return fs.ModeSymlink
	case unixpb.Data_File, unixpb.Data_Raw:
		return fs.FileMode(0)
	default:
		return fs.ModeIrregular
	}
}
