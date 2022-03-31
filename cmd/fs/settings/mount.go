package settings

import (
	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	goparams "github.com/djdv/go-filesystem-utils/internal/parameters/reflect"
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
	return goparams.NewParameter(
		"Host system API to use.",
		goparams.WithName("system"),
	)
}

func SystemID() parameters.Parameter {
	return goparams.NewParameter(
		"Target FS to use.",
		goparams.WithName("fs"),
	)
}

func IPFS() parameters.Parameter {
	return goparams.NewParameter(
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
	return goparams.NewParameter(
		"Unmount all mountpoints.",
		goparams.WithAlias("a"),
	)
}
