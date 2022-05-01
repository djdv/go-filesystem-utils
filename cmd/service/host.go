package service

import (
	"os"
	"reflect"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	"github.com/kardianos/service"
)

func serviceConfig(settings *Settings) *service.Config {
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
func serviceArgs() []string {
	var (
		args   = []string{Name}
		params = []string{
			settings.APIParam().Name(parameters.CommandLine),
			settings.AutoExitParam().Name(parameters.CommandLine),
		}
	)
	// TODO: reconsider this. I'm pretty sure it will be fine to use the parsed form now.
	// Pretty much all the ambiguity has been removed at higher levels now.
	// We can more easily work with real structured data than mixed strings+data.
	//
	// NOTE: We copy program arguments exactly as they were supplied,
	// rather that copying their parsed form.
	// This is so that subsequent invocations processed/expanded them again.
	// I.e. when the service is started, it will expand arguments then,
	// rather than using whatever was parsed now.
	// (Consider arguments that contain dynamic environment variables.)
	for i, arg := range os.Args {
		for _, param := range params {
			if strings.HasPrefix(
				strings.TrimLeft(arg, "-"),
				param,
			) {
				// handle unbroken arguments: `--parameter=argument`
				args = append(args, arg)
				// handle argument portion of separated arguments: `--parameter argument`
				if !strings.Contains(arg, "=") {
					// XXX: This should be validated up front by the cmds lib,
					// but if it's not - we could potentially panic via out of bounds.
					args = append(args, os.Args[i+1])
				}
			}
		}
	}
	return args
}
