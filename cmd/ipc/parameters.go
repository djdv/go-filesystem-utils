package ipc

import (
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/multiformats/go-multiaddr"
)

type HostService struct {
	Username string `settings:"arguments"`
	PlatformSettings
}

func (*HostService) Parameters() parameters.Parameters {
	var (
		pkg = []parameters.Parameter{
			Username(),
		}
		system = (*PlatformSettings)(nil).Parameters()
	)
	return append(pkg, system...)
}

func Username() parameters.Parameter {
	return parameters.NewParameter(
		"Username to use when interfacing with the system service manager.",
	)
}

type MountSettings struct {
	HostAPI   filesystem.API `settings:"arguments"`
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
	return parameters.NewParameter(
		"Host system API to use.",
		parameters.WithName("system"),
	)
}

func SystemID() parameters.Parameter {
	return parameters.NewParameter(
		"Target FS to use.",
		parameters.WithName("fs"),
	)
}

func IPFS() parameters.Parameter {
	return parameters.NewParameter(
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
	return parameters.NewParameter(
		"Unmount all mountpoints.", // TODO: WithShortFlag -a
	)
}
