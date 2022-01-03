package status

import (
	"context"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	"github.com/djdv/go-filesystem-utils/cmd/service/host"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

type (
	Host     = host.Settings
	Settings struct {
		fscmds.Settings
		Host
	}
)

func (*Settings) Parameters() parameters.Parameters {
	var (
		root   = (*fscmds.Settings)(nil).Parameters()
		system = (*host.Settings)(nil).Parameters()
	)
	return append(root, system...)
}

func parseSettings(ctx context.Context, request *cmds.Request) (*Settings, error) {
	var (
		settings        = new(Settings)
		unsetArgs, errs = parameters.ParseSettings(ctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
	)
	if _, err := parameters.AccumulateArgs(ctx, unsetArgs, errs); err != nil {
		return nil, err
	}
	return settings, nil
}
