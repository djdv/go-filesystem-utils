package host

import (
	"context"

	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings/runtime"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

type Settings struct {
	Username string `settings:"arguments"`
	ServiceName,
	ServiceDisplayName,
	ServiceDescription string

	PlatformSettings
}

func (*Settings) Parameters(ctx context.Context) parameters.Parameters {
	partialParams := []runtime.CmdsParameter{
		{HelpText: "Username to use when interfacing with the system service manager."},
		{HelpText: "Service name (usually as a command argument) to associate with the service (when installing)"},
		{HelpText: "Service display name (usually seen in UI labels) to associate with the service (when installing)"},
		{HelpText: "Description (usually seen in UI labels) to associate with the service (when installing)"},
	}
	return CtxJoin(ctx,
		runtime.GenerateParameters[Settings](ctx, partialParams),
		(*PlatformSettings).Parameters(nil, ctx),
	)
}
