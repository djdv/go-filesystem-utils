package service

import (
	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
)

type Settings struct {
	fscmds.Settings
	Username string
	PlatformSettings
}

func (*Settings) Parameters() parameters.Parameters {
	var (
		root   = fscmds.Parameters()
		pkg    = Parameters()
		system = (*PlatformSettings)(nil).Parameters()
	)
	return append(root, append(pkg, system...)...)
}

func Parameters() parameters.Parameters {
	return parameters.Parameters{
		Username(),
	}
}

func Username() parameters.Parameter {
	return parameters.NewParameter(
		"Username to use when interfacing with the system service manager.",
	)
}
