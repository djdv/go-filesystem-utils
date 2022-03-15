package daemon

import (
	"github.com/djdv/go-filesystem-utils/cmd/fs/settings"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type Settings struct {
	settings.Settings
}

func (*Settings) Parameters() parameters.Parameters {
	return (*settings.Settings)(nil).Parameters()
}
