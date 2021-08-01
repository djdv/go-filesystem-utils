package ipfscore

import (
	"context"
	"fmt"
	gopath "path"
	"strings"

	"github.com/ipfs/go-ipfs/filesystem"
	interfaceutils "github.com/ipfs/go-ipfs/filesystem/interface"
	iferrors "github.com/ipfs/go-ipfs/filesystem/interface/errors"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	corepath "github.com/ipfs/interface-go-ipfs-core/path"
)

var errReadOnly = fmt.Errorf("read only FS, modification %w",
	iferrors.UnsupportedRequest(), // "operation not supported by this node" or similar
)

type coreInterface struct {
	ctx      context.Context
	core     interfaceutils.CoreExtender
	systemID filesystem.ID
}

func NewInterface(ctx context.Context, core coreiface.CoreAPI, systemID filesystem.ID) filesystem.Interface {
	return &coreInterface{
		ctx:      ctx,
		core:     &interfaceutils.CoreExtended{CoreAPI: core},
		systemID: systemID,
	}
}

func (ci *coreInterface) ID() filesystem.ID     { return ci.systemID }
func (*coreInterface) Close() error             { return nil }
func (*coreInterface) Rename(_, _ string) error { return errReadOnly }

func (ci *coreInterface) joinRoot(path string) corepath.Path {
	return corepath.New(gopath.Join("/", strings.ToLower(ci.systemID.String()), path))
}
