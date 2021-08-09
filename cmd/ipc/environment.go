package ipc

import (
	"context"
	"reflect"

	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
)

type (
	Environment interface {
		ServiceConfig(*cmds.Request) (*service.Config, error)
	}

	daemonEnvironment struct {
		context.Context
	}
)

func MakeEnvironment(ctx context.Context, request *cmds.Request) (cmds.Environment, error) {
	env := &daemonEnvironment{
		Context: ctx,
	}
	return env, nil
}

func CastEnvironment(environment cmds.Environment) (Environment, error) {
	typedEnv, isUsable := environment.(Environment)
	if !isUsable {
		interfaceName := reflect.TypeOf((*Environment)(nil)).Elem().Name()
		return nil, cmds.Errorf(cmds.ErrImplementation,
			"%T does not implement the %s interface",
			environment, interfaceName,
		)
	}
	return typedEnv, nil
}

func (env *daemonEnvironment) ServiceConfig(request *cmds.Request) (*service.Config, error) {
	var (
		ctx             = request.Context
		settings        = new(HostService)
		unsetArgs, errs = parameters.ParseSettings(ctx, settings,
			parameters.SettingsFromCmds(request),
			parameters.SettingsFromEnvironment(),
		)
	)
	if _, err := parameters.AccumulateArgs(ctx, unsetArgs, errs); err != nil {
		return nil, err
	}
	return &service.Config{
		Name:        ServiceName,
		DisplayName: ServiceDisplayName,
		Description: ServiceDescription,
		UserName:    settings.Username,
		Option:      serviceKeyValueFrom(&settings.PlatformSettings),
		Arguments:   serviceArgs(),
	}, nil
}
