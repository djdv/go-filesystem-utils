package ipc

import (
	"os"
	"reflect"
	"strings"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/kardianos/service"
)

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

// serviceArgs constructs command line arguments,
// extracting service-relevant arguments from the current process arguments.
// The caller should store them in the service.Config,
// so that the service manager can use them when starting the process itself.
func serviceArgs() (serviceArgs []string) {
	serviceArgs = []string{ServiceCommandName}
	var (
		params = []string{
			fscmds.ServiceMaddrs().CommandLine(),
			fscmds.AutoExitInterval().CommandLine(),
		}
	)
	// NOTE: We do not marshal potentially processed values back into their argument form.
	// We copy the arguments from argv exactly as they were supplied.
	for i, arg := range os.Args {
		for _, param := range params {
			if strings.HasPrefix(
				strings.TrimLeft(arg, "-"),
				param,
			) {
				// handle unbroken arguments: `--parameter=argument`
				serviceArgs = append(serviceArgs, arg)
				// handle argument portion of seperated arguments: `--parameter argument`
				if !strings.Contains(arg, "=") {
					// XXX: This should be validated up front by the cmds lib,
					// but if it's not - we could potentially panic via out of bounds.
					serviceArgs = append(serviceArgs, os.Args[i+1])
				}
			}
		}
	}
	return serviceArgs
}

// NOTE: Field names and data types in the setting's struct declaration -
// must match the map key names defined in the `service.KeyValue` pkg documentation.
func serviceKeyValueFrom(platformSettings *PlatformSettings) service.KeyValue {
	var (
		settingsValue   = reflect.ValueOf(platformSettings).Elem()
		settingsType    = settingsValue.Type()
		settingsCount   = settingsType.NumField()
		serviceSettings = make(service.KeyValue, settingsCount)
	)
	for i := 0; i != settingsCount; i++ {
		var (
			structField = settingsType.Field(i)
			fieldValue  = settingsValue.Field(i)
		)
		serviceSettings[structField.Name] = fieldValue.Interface()
	}
	return serviceSettings
}
