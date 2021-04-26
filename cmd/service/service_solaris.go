package service

import fscmds "github.com/djdv/go-filesystem-utils/cmd"

var (
	PrefixParameter = fscmds.CmdsParameterSet{
		Name:        "prefix",
		Description: "Service FMRI prefix.",
		Environment: "FS_SERVICE_PREFIX",
	}

	servicePlatformOptions = platformOptions{
		stringOptions: append([]fscmds.CmdsParameterSet{
			PrefixParameter,
		},
			servicePosixOptions.stringOptions...,
		),
		intOptions:  servicePosixOptions.intOptions,
		boolOptions: servicePosixOptions.boolOptions,
	}
)
