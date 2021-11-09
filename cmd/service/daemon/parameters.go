package daemon

import (
	"context"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

type Settings struct {
	fscmds.Settings
	// TODO: --file-ids=[]FID{$fileSocket, $tcpListener, ...} <- come from os/service manager
	// ^ current workaround is using cmds.request.extra["magic"] <- remove this for that ^
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
