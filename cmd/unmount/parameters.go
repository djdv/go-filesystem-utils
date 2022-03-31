package unmount

import (
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type Settings struct {
	settings.Settings
	settings.UnmountSettings
}

func (self *Settings) Parameters() parameters.Parameters {
	var (
		root = (*settings.Settings)(nil).Parameters()
		pkg  = (*settings.UnmountSettings)(nil).Parameters()
	)
	return append(root, pkg...)
}
