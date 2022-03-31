package status

import (
	"github.com/djdv/go-filesystem-utils/cmd/service/host"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type (
	Host     = host.Settings
	Settings struct {
		settings.Settings
		Host
	}
)

func (*Settings) Parameters() parameters.Parameters {
	var (
		root   = (*settings.Settings)(nil).Parameters()
		system = (*host.Settings)(nil).Parameters()
	)
	return append(root, system...)
}
