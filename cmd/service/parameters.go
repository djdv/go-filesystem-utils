package service

import (
	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
)

type Settings struct {
	fscmds.Settings
	ipc.HostService
}

func (*Settings) Parameters() parameters.Parameters {
	var (
		root   = (*fscmds.Settings)(nil).Parameters()
		system = (*ipc.HostService)(nil).Parameters()
	)
	return append(root, system...)
}
