package host

import (
	"github.com/djdv/go-filesystem-utils/internal/cmdslib"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	"golang.org/x/net/context"
)

type PlatformSettings struct {
	ServicePassword  string
	DelayedAutoStart bool
}

func (*PlatformSettings) Parameters(ctx context.Context) parameters.Parameters {
	partialParams := []cmdslib.CmdsParameter{
		{HelpText: "Password to use when interfacing with the system service manager."},
		{HelpText: "Prevent the service from starting immediately after booting."},
	}
	return cmdslib.ReflectParameters[PlatformSettings](ctx, partialParams)
}
