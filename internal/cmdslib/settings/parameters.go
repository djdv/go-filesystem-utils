package settings

import (
	"context"
	"log"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/cmdslib"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	"github.com/multiformats/go-multiaddr"
)

// Settings implements the `parameters.Settings` interface
// to generate parameter getters and setters.
type Settings struct {
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

const (
	// TODO: names
	APIParam      = "api"
	AutoExitParam = "auto-exit-interval"
)

func (*Settings) Parameters(ctx context.Context) parameters.Parameters {
	partialParams := []cmdslib.CmdsParameter{
		{
			OptionName: APIParam,
			HelpText:   "File system service multiaddr to use.",
		},
		{
			OptionName: AutoExitParam,
			HelpText:   `Time interval (e.g. "30s") to check if the service is active and exit if not.`,
		},
	}
	return cmdslib.ReflectParameters[Settings](ctx, partialParams)
}

type HostService struct {
	Username string `settings:"arguments"`
	PlatformSettings
}

func (*HostService) Parameters(ctx context.Context) parameters.Parameters {
	partialParams := []cmdslib.CmdsParameter{
		{HelpText: "Username to use when interfacing with the system service manager."},
	}
	log.Println("host trace:", partialParams)
	//FIXME: we need to pass all child-params to Reflect
	// I.e. .Parameters needs for be aggregated and merged into partialPArams
	// and our params need to be appended|prepended

	/*
		return cmdslib.PrependParameters(ctx,
			cmdslib.ReflectParameters[HostService](ctx, partialParams),
			(*PlatformSettings).Parameters(nil, ctx),
		)
	*/
	/*
		combined := cmdslib.PrependParameters(ctx,
			partialParams,
			(*PlatformSettings).Parameters(nil, ctx),
		)
		return cmdslib.ReflectParameters[HostService](ctx, combined)
	*/

	//ours := cmdslib.ReflectParameters[HostService](ctx, partialParams)

	/*
		transform []CmdsParameter into <-chan Parameter
		Join with sub.params if any
		make sure Reflect() is surface level only, we'll expect sub.Params to be correct.
	*/

}
