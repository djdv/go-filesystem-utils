package host

import (
	"os"
	"reflect"
	"strings"

	"github.com/djdv/go-filesystem-utils/cmd/service/daemon"
	fscmds "github.com/djdv/go-filesystem-utils/filesystem/cmds"
	"github.com/kardianos/service"
)

func ServiceConfig(settings *Settings) *service.Config {
	var (
		username    = settings.Username
		name        = settings.ServiceName
		displayName = settings.ServiceDisplayName
		description = settings.ServiceDescription
	)
	if name == "" {
		name = "go-filesystem"
	}
	if displayName == "" {
		displayName = "Go File system service"
	}
	if description == "" {
		description = "Manages Go file system instances."
	}

	return &service.Config{
		Name:        name,
		DisplayName: displayName,
		Description: description, // TODO: from option
		UserName:    username,
		Option:      serviceKeyValueFrom(&settings.PlatformSettings),
		Arguments:   serviceArgs(),
	}
}

func serviceKeyValueFrom(platformSettings *PlatformSettings) service.KeyValue {
	var (
		// NOTE: Field names and data types in this setting's struct's declaration;
		// must match the map key names and types defined in the `service.KeyValue` pkg documentation.
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

// serviceArgs constructs command line arguments,
// extracting service-relevant arguments from the current process arguments.
// The caller should store them in the service.Config,
// so that the service manager can use them when starting the process itself.
func serviceArgs() (serviceArgs []string) {
	serviceArgs = daemon.CmdsPath()
	serviceArgs = serviceArgs[:len(serviceArgs)-1]
	params := []string{
		fscmds.ServiceMaddrs().CommandLine(),
		fscmds.AutoExitInterval().CommandLine(),
	}
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
				// handle argument portion of separated arguments: `--parameter argument`
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
