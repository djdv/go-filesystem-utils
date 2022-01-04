package mount

import (
	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/filesystem"
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
