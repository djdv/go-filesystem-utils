package settings

import (
	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	"github.com/multiformats/go-multiaddr"
)

type MountSettings struct {
	HostAPI   filesystem.API `parameters:"settings"`
	FSID      filesystem.ID
	IPFSMaddr multiaddr.Multiaddr
}

func (*MountSettings) Parameters() parameters.Parameters {
	return []parameters.Parameter{
		SystemAPI(),
		SystemID(),
		IPFS(),
	}
}

func SystemAPI() parameters.Parameter {
	return cmdslib.NewParameter(
		"Host system API to use.",
		cmdslib.WithName("system"),
	)
}

func SystemID() parameters.Parameter {
	return cmdslib.NewParameter(
		"Target FS to use.",
		cmdslib.WithName("fs"),
	)
}

func IPFS() parameters.Parameter {
	return cmdslib.NewParameter(
		"IPFS multiaddr to use.",
	)
}

type UnmountSettings struct {
	MountSettings
	All bool
}

func (*UnmountSettings) Parameters() parameters.Parameters {
	return append((*MountSettings)(nil).Parameters(),
		All(),
	)
}

func All() parameters.Parameter {
	return cmdslib.NewParameter(
		"Unmount all mountpoints.",
		cmdslib.WithAlias("a"),
	)
}
