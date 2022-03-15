package service

import (
	"github.com/djdv/go-filesystem-utils/cmd/fs/settings"
	"github.com/djdv/go-filesystem-utils/cmd/service/host"
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
		system = (*host.Settings)(nil).Parameters()
		root   = (*settings.Settings)(nil).Parameters()
	)
	return append(root, system...)
}
