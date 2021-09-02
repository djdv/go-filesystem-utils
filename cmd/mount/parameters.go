package mount

import (
	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/ipc"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
)

type Settings struct {
	fscmds.Settings
	ipc.MountSettings
}

func (*Settings) Parameters() parameters.Parameters {
	var (
		root = (*fscmds.Settings)(nil).Parameters()
		pkg  = (*ipc.MountSettings)(nil).Parameters()
	)
	return append(root, pkg...)
}
