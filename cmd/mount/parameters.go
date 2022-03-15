package mount

import (
	"github.com/djdv/go-filesystem-utils/cmd/fs/settings"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type Settings struct {
	settings.Settings
	settings.MountSettings
}

func (self *Settings) Parameters() parameters.Parameters {
	var (
		root = (*settings.Settings)(nil).Parameters()
		pkg  = (*settings.MountSettings)(nil).Parameters()
	)
	return append(root, pkg...)
}
