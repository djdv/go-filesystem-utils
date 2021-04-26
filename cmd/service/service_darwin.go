package service

import fscmds "github.com/djdv/go-filesystem-utils/cmd"

var (
	LaunchdConfigParameter = fscmds.CmdsParameterSet{
		Name:        "LaunchdConfig",
		Description: "Use custom launchd config",
		Environment: "FS_SERVICE_LAUNCHD",
	}
	KeepAliveParameter = fscmds.CmdsParameterSet{
		Name:        "KeepAlive",
		Description: " Prevent the system from stopping the service automatically.",
		Environment: "FS_SERVICE_KEEPALIVE",
	}
	RunAtLoadParameter = fscmds.CmdsParameterSet{
		Name:        "RunAtLoad",
		Description: "Run the service after its job has been loaded.",
		Environment: "FS_SERVICE_RUNATLOAD",
	}
	SessionCreateParameter = fscmds.CmdsParameterSet{
		Name:        "SessionCreate",
		Description: "Create a full user session.",
		Environment: "FS_SERVICE_FULLUSER",
	}

	servicePlatformOptions = platformOptions{
		stringOptions: append([]fscmds.CmdsParameterSet{
			LaunchdConfigParameter,
		},
			servicePosixOptions.stringOptions...,
		),
		intOptions: servicePosixOptions.intOptions,
		boolOptions: append([]fscmds.CmdsParameterSet{
			KeepAliveParameter,
			RunAtLoadParameter,
			SessionCreateParameter,
		},
			servicePosixOptions.boolOptions...,
		),
	}
)
