package settings

import (
	"context"
	"reflect"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	"github.com/djdv/go-filesystem-utils/internal/parameters/environment"
	goparams "github.com/djdv/go-filesystem-utils/internal/parameters/reflect"
	"github.com/djdv/go-filesystem-utils/internal/parameters/reflect/cmds/arguments"
	"github.com/djdv/go-filesystem-utils/internal/parameters/reflect/cmds/options"
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

//func ParseAll[settings any](ctx context.Context,
func ParseAll[settings any, setIntf goparams.SettingsConstraint[settings]](ctx context.Context,
	request *cmds.Request) (*settings, error) {
	var (
		typeHandlers = handlers()
		sources      = []arguments.SetFunc{
			arguments.SettingsFromCmds(request),
			environment.SettingsFromEnvironment(),
		}
	)
	return arguments.Parse[settings, setIntf](ctx, sources, typeHandlers...)
}

// TODO: Name.
func handlers() []goparams.TypeParser {
	return []goparams.TypeParser{
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

func MakeOptions[settings any](opts ...options.CmdsOptionOption) []cmds.Option {
	return options.MustMakeCmdsOptions[Settings](append(optionMakers(), opts...)...)
	//return options.MustMakeCmdsOptions(empty, append(optionMakers(), options...)...)
}

func optionMakers() []options.CmdsOptionOption {
	var (
		makers = []options.OptionMaker{
			{
				Type:           reflect.TypeOf((*multiaddr.Multiaddr)(nil)).Elem(),
				MakeOptionFunc: cmds.StringOption,
			},
			{
				Type:           reflect.TypeOf((*time.Duration)(nil)).Elem(),
				MakeOptionFunc: cmds.StringOption,
			},
			{
				Type:           reflect.TypeOf((*filesystem.ID)(nil)).Elem(),
				MakeOptionFunc: cmds.StringOption,
			},
			{
				Type:           reflect.TypeOf((*filesystem.API)(nil)).Elem(),
				MakeOptionFunc: cmds.StringOption,
			},
		}
		opts = make([]options.CmdsOptionOption, len(makers))
	)
	for i, maker := range makers {
		opts[i] = options.WithMaker(maker)
	}
	return opts
}

func ServiceMaddrs() parameters.Parameter {
	return goparams.NewParameter(
		"File system service multiaddr to use.",
		goparams.WithRootNamespace(),
		goparams.WithName("api"),
	)
}

func AutoExitInterval() parameters.Parameter {
	return goparams.NewParameter(
		`Time interval (e.g. "30s") to check if the service is active and exit if not.`,
		goparams.WithRootNamespace(),
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
	return goparams.NewParameter(
		"Username to use when interfacing with the system service manager.",
	)
}
