package ipc

import (
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
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
