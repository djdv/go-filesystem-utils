package mount

import (
	"github.com/djdv/go-filesystem-utils/filesystem/cmds"
	fscmds "github.com/djdv/go-filesystem-utils/filesystem/cmds"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
)

type Settings struct {
	fscmds.Settings
	filesystem.MountSettings
}

func (*Settings) Parameters() parameters.Parameters {
	var (
		root = (*fscmds.Settings)(nil).Parameters()
		pkg  = (*filesystem.MountSettings)(nil).Parameters()
	)
	return append(root, pkg...)
}
