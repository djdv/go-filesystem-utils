package ipc

import (
	"os"
	"reflect"
	"strings"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/kardianos/service"
	"github.com/multiformats/go-multiaddr"
)

type (
	SystemController struct {
		service.Status
		Error error
	}

	// TODO: Text encoder.
	ServiceStatus struct {
		Listeners []multiaddr.Multiaddr
		SystemController
	}
)

func serviceArgs(settings *HostService) (serviceArgs []string) {
	serviceArgs = []string{ServiceCommandName}
	if len(settings.ServiceMaddrs) > 0 {
		// Copy service-relevant arguments from our process,
		// into the service config. The service manager will
		// use these when starting its own process.
		apiParam := fscmds.ServiceMaddrs().CommandLine()
		for _, arg := range os.Args {
			if strings.HasPrefix(
				strings.TrimLeft(arg, "-"),
				apiParam,
			) {
				serviceArgs = append(serviceArgs, arg)
			}
		}
	}
	if settings.AutoExitInterval != 0 {
		exitParam := fscmds.AutoExitInterval().CommandLine()
		for _, arg := range os.Args {
			if strings.HasPrefix(arg, exitParam) {
				serviceArgs = append(serviceArgs, arg)
			}
		}
	}
	return serviceArgs
}

// NOTE: Field names and data types in the setting's struct declaration
// must match the map key names defined in the `service.KeyValue` pkg documentation.
func serviceKeyValueFrom(platformSettings *PlatformSettings) service.KeyValue {
	var (
		settingsValue   = reflect.ValueOf(platformSettings).Elem()
		settingsType    = settingsValue.Type()
		settingsCount   = settingsType.NumField()
		serviceSettings = make(service.KeyValue, settingsCount)
	)
	for i := 0; i != settingsCount; i++ {
		structField := settingsType.Field(i) // The field itself (for its name).
		fieldValue := settingsValue.Field(i) // The value it holds (not its type name).
		serviceSettings[structField.Name] = fieldValue.Interface()
	}
	return serviceSettings
}
