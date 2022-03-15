package settings

import (
	"context"
	"reflect"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

// Settings implements the `parameters.Settings` interface
// to generate parameter getters and setters.
type Settings struct {
	// ServiceMaddrs is a list of addresses to server instances.
	// Commands are free to use this value for any purpose as a context hint.
	// E.g. It may be used to connect to existing servers,
	// spawn new ones, check the status of them, etc.
	ServiceMaddrs []multiaddr.Multiaddr `parameters:"settings"`
	// AutoExitInterval will cause processes spawned by this command
	// to exit (if not busy) after some interval.
	// If the process remains busy, it will remain running
	// until another stop condition is met.
	AutoExitInterval time.Duration
}

// Parameters returns the list of parameters associated with this pkg.
func (*Settings) Parameters() parameters.Parameters {
	return parameters.Parameters{
		ServiceMaddrs(),
		AutoExitInterval(),
	}
}

func handlers() []parameters.TypeParser {
	var (
		maddrType   = reflect.TypeOf((*multiaddr.Multiaddr)(nil)).Elem()
		maddrParser = func(argument string) (interface{}, error) {
			return multiaddr.NewMultiaddr(argument)
		}
		durationType   = reflect.TypeOf((*time.Duration)(nil)).Elem()
		durationParser = func(argument string) (interface{}, error) {
			return time.ParseDuration(argument)
		}
	)
	return []parameters.TypeParser{
		{
			Type:      maddrType,
			ParseFunc: maddrParser,
		},
		{
			Type:      durationType,
			ParseFunc: durationParser,
		},
	}
}

type SetConstraint[structPtr any] interface {
	parameters.Settings
}

func ParseAll[setPtr SetConstraint[setPtr]](ctx context.Context,
	empty setPtr, request *cmds.Request) error {
	var (
		typeHandlers = handlers()
		sources      = []parameters.SetFunc{
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		}
	)
	return parameters.Parse(ctx, empty, sources, typeHandlers...)
}

func ServiceMaddrs() parameters.Parameter {
	return parameters.NewParameter(
		"File system service multiaddr to use.",
		parameters.WithRootNamespace(),
		parameters.WithName("api"),
	)
}

func AutoExitInterval() parameters.Parameter {
	return parameters.NewParameter(
		`Time interval (e.g. "30s") to check if the service is active and exit if not.`,
		parameters.WithRootNamespace(),
	)
}

type HostService struct {
	Username string `settings:"arguments"`
	PlatformSettings
}

func (*HostService) Parameters() parameters.Parameters {
	var (
		pkg = []parameters.Parameter{
			Username(),
		}
		system = (*PlatformSettings)(nil).Parameters()
	)
	return append(pkg, system...)
}

func Username() parameters.Parameter {
	return parameters.NewParameter(
		"Username to use when interfacing with the system service manager.",
	)
}
