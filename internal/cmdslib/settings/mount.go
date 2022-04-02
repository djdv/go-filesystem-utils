package settings

import (
	"context"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	"github.com/multiformats/go-multiaddr"
)

type MountSettings struct {
	HostAPI   filesystem.API `parameters:"settings"`
	FSID      filesystem.ID
	IPFSMaddr multiaddr.Multiaddr
}

const (
	// TODO: better names
	HostAPIParam      = "system"
	FileSystemIDParam = "fs"
)

func (*MountSettings) Parameters(ctx context.Context) parameters.Parameters {
	partialParams := []cmdslib.CmdsParameter{
		{
			OptionName: HostAPIParam,
			HelpText:   "Host system API to use.",
		},
		{
			OptionName: FileSystemIDParam,
			HelpText:   "Target FS to use.",
		},
		{
			OptionName: "ipfs",
			HelpText:   "IPFS multiaddr to use.",
		},
	}
	return cmdslib.ReflectParameters[MountSettings](ctx, partialParams)
}

type UnmountSettings struct {
	MountSettings
	All bool
}

func (*UnmountSettings) Parameters(ctx context.Context) parameters.Parameters {
	partialParams := []cmdslib.CmdsParameter{
		{
			OptionAliases: []string{"a"},
			HelpText:      "Unmount all mountpoints.",
		},
	}
	return CtxJoin(ctx,
		cmdslib.ReflectParameters[UnmountSettings](ctx, partialParams),
		(*MountSettings).Parameters(nil, ctx),
	)
}
