package settings

import (
	"time"

	"github.com/djdv/go-filesystem-utils/internal/cmdslib"
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

// Parameters returns the list of parameters associated with this pkg.
func (*Settings) Parameters() parameters.Parameters {
	return parameters.Parameters{
		ServiceMaddrs(),
		AutoExitInterval(),
	}
}

func ServiceMaddrs() parameters.Parameter {
	return cmdslib.NewParameter(
		"File system service multiaddr to use.",
		cmdslib.WithRootNamespace(),
		cmdslib.WithName("api"),
	)
}

func AutoExitInterval() parameters.Parameter {
	return cmdslib.NewParameter(
		`Time interval (e.g. "30s") to check if the service is active and exit if not.`,
		cmdslib.WithRootNamespace(),
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
	return cmdslib.NewParameter(
		"Username to use when interfacing with the system service manager.",
	)
}
