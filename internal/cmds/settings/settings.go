package settings

import (
	"context"
	"reflect"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/arguments"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/environment"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
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
	return runtime.MustMakeParameters[*Root](ctx, partialParams)
}

func Parse[setIntf runtime.SettingsConstraint[settings], settings any](ctx context.Context,
	request *cmds.Request,
) (*settings, error) {
	var (
		typeHandlers = handlers()
		sources      = []runtime.SetFunc{
			arguments.SettingsFromCmds(request),
			environment.SettingsFromEnvironment(),
		}
	)
	return runtime.Parse[setIntf](ctx, sources, typeHandlers...)
}

// TODO: Name.
func handlers() []runtime.TypeParser {
	return []runtime.TypeParser{
		{
			Type: reflect.TypeOf((*multiaddr.Multiaddr)(nil)).Elem(),
			ParseFunc: func(argument string) (interface{}, error) {
				return multiaddr.NewMultiaddr(argument)
			},
		},
		{
			Type: reflect.TypeOf((*time.Duration)(nil)).Elem(),
			ParseFunc: func(argument string) (interface{}, error) {
				return time.ParseDuration(argument)
			},
		},
		{
			Type: reflect.TypeOf((*filesystem.ID)(nil)).Elem(),
			ParseFunc: func(argument string) (interface{}, error) {
				return filesystem.StringToID(argument)
			},
		},
		{
			Type: reflect.TypeOf((*filesystem.API)(nil)).Elem(),
			ParseFunc: func(argument string) (interface{}, error) {
				return filesystem.StringToAPI(argument)
			},
		},
	}
}
