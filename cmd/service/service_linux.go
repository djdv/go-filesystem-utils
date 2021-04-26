package service

import fscmds "github.com/djdv/go-filesystem-utils/cmd"

var (
	OpenRCScriptParameter = fscmds.CmdsParameterSet{
		Name:        "OpenRCScript",
		Description: "Use custom OpenRCS script",
		Environment: "FS_SERVICE_OPENRC",
	}
	LimitNOFILEParameter = fscmds.CmdsParameterSet{
		Name:        "LimitNOFILE",
		Description: "Maximum open files (ulimit -n) (https://serverfault.com/questions/628610/increasing-nproc-for-processes-launched-by-systemd-on-centos-7)",
		Environment: "FS_SERVICE_NOFILE",
	}

	servicePlatformOptions = platformOptions{
		stringOptions: append([]fscmds.CmdsParameterSet{
			OpenRCScriptParameter,
		},
			servicePosixOptions.stringOptions...,
		),
		intOptions: append([]fscmds.CmdsParameterSet{
			LimitNOFILEParameter,
		},
			servicePosixOptions.intOptions...,
		),
		boolOptions: servicePosixOptions.boolOptions,
	}
)
