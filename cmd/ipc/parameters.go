package ipc

import (
	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
)

type HostService struct {
	fscmds.Settings
	Username string
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
