package daemon

import (
	"context"

	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	fscmds "github.com/djdv/go-filesystem-utils/filesystem/cmds"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

type Settings struct {
	fscmds.Settings
}

func (*Settings) Parameters() parameters.Parameters {
	return (*fscmds.Settings)(nil).Parameters()
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
