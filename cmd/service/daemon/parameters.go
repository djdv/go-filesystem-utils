package daemon

import (
	"context"

	"github.com/djdv/go-filesystem-utils/cmd/environment"
	"github.com/djdv/go-filesystem-utils/cmd/fs/settings"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

type (
	errCh = <-chan error

	Settings struct {
		settings.Settings
	}
)

func (*Settings) Parameters() parameters.Parameters {
	return (*settings.Settings)(nil).Parameters()
}

func parseCmds(ctx context.Context, request *cmds.Request,
	env cmds.Environment) (*Settings, environment.Environment, error) {
	var (
		daemonSettings = new(Settings)
		err            = settings.ParseAll(ctx, daemonSettings, request)
	)
	if err != nil {
		return nil, nil, err
	}
	serviceEnv, err := environment.Assert(env)
	if err != nil {
		return nil, nil, err
	}
	return daemonSettings, serviceEnv, nil
}

func settingsToListeners(ctx context.Context,
	request *cmds.Request, settings *Settings) (listeners, errCh, error) {
	var (
		listeners = make([]listeners, 0, 3)
		errChans  = make([]errCh, 0, 1)
	)
	hostListeners, err := hostListenersFromRequest(request)
	if err != nil {
		return nil, nil, err
	}
	if hostListeners != nil {
		listeners = append(listeners, generate(ctx, hostListeners...))
	}

	serviceMaddrs := settings.ServiceMaddrs
	if serviceMaddrs != nil {
		var (
			maddrs                = generate(ctx, serviceMaddrs...)
			argListeners, argErrs = listenersFromMaddrs(ctx, maddrs)
		)
		listeners = append(listeners, argListeners)
		errChans = append(errChans, argErrs)
	}

	useDefaults := len(listeners) == 0
	if useDefaults {
		userMaddrs, err := UserServiceMaddrs()
		if err != nil {
			return nil, nil, err
		}
		var (
			maddrs                        = generate(ctx, userMaddrs...)
			defaultListeners, defaultErrs = initializeAndListen(ctx, maddrs)
		)
		listeners = append(listeners, defaultListeners)
		errChans = append(errChans, defaultErrs)
	}
	return CtxMerge(ctx, listeners...), CtxMerge(ctx, errChans...), nil
}
