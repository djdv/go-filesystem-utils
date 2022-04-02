package service

import (
	"context"

	"github.com/djdv/go-filesystem-utils/cmd/service/host"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

//FIXME: `onlyRootOptions` will skip fields when we try to make options for [Settings]
type (
	Host     = host.Settings
	Settings struct {
		Host
		settings.Settings
	}
)

func (*Settings) Parameters(ctx context.Context) parameters.Parameters {
	return CtxJoin(ctx,
		(*host.Settings).Parameters(nil, ctx),
		(*settings.Settings).Parameters(nil, ctx),
	)
}
