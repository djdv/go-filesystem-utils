package settings

import (
	"context"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	"github.com/multiformats/go-multiaddr"
)

const (
	// TODO: names
	APIParam      = "api"
	AutoExitParam = "auto-exit-interval"
)

type Root struct {
	// ServiceMaddrs is a list of addresses to server instances.
	// Commands are free to use this value for any purpose as a context hint.
	// E.g. It may be used to connect to existing servers,
	// spawn new ones, check the status of them, etc.
	ServiceMaddrs []multiaddr.Multiaddr
	// AutoExitInterval will cause processes spawned by this command
	// to exit (if not busy) after some interval.
	// If the process remains busy, it will remain running
	// until another stop condition is met.
	AutoExitInterval time.Duration
}

func (*Root) Parameters(ctx context.Context) parameters.Parameters {
	partialParams := []runtime.CmdsParameter{
		{
			OptionName: APIParam,
			HelpText:   "File system service multiaddr to use.",
		},
		{
			OptionName: AutoExitParam,
			HelpText:   `Time interval (e.g. "30s") to check if the service is active and exit if not.`,
		},
	}
	return runtime.GenerateParameters[Root](ctx, partialParams)
}
