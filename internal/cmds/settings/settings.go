package settings

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/environment"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/request"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

type Root struct {
	// ServiceMaddrs is a list of addresses to service instances.
	// Commands are free to use this value for any purpose.
	// E.g. It may be used to connect to existing servers,
	// spawn new ones, check the status of them, etc.
	ServiceMaddrs []multiaddr.Multiaddr
	// AutoExitInterval will cause processes spawned by commands
	// to exit (if not busy) after some interval.
	// If the process remains busy, it will remain running
	// until another stop condition is met.
	AutoExitInterval time.Duration
}

func (*Root) Parameters(ctx context.Context) parameters.Parameters {
	var (
		constructors = []func() parameters.Parameter{
			APIParam,
			AutoExitParam,
		}
		out = make(chan parameters.Parameter, len(constructors))
	)
	go func() {
		defer close(out)
		for _, paramGen := range constructors {
			if ctx.Err() != nil {
				return
			}
			out <- paramGen()
		}
	}()
	return out
}

// TODO: should runtime expose a function which gives us consistent defaults?
// i.e. export `runtime.programMetadata`
func execName() string {
	progName := filepath.Base(os.Args[0])
	return strings.TrimSuffix(progName, filepath.Ext(progName))
}

func APIParam() parameters.Parameter {
	return runtime.CmdsParameter{
		OptionName: "api",
		HelpText:   "File system service multiaddrs to use.",
		EnvPrefix:  execName(),
	}
}

func AutoExitParam() parameters.Parameter {
	return runtime.CmdsParameter{
		OptionName: "auto exit interval",
		HelpText:   `Check every time interval (e.g. "30s") and stop the service if it's not active`,
		EnvPrefix:  execName(),
	}
}

func Parse[setIntf runtime.SettingsConstraint[settings], settings any](ctx context.Context,
	req *cmds.Request,
) (*settings, error) {
	var (
		typeHandlers = handlers()
		sources      = []runtime.SetFunc{
			request.ValueSource(req),
			environment.ValueSource(),
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
