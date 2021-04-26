//go:build !windows
// +build !windows

package service

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	fscmds "github.com/djdv/go-filesystem-utils/cmd"
)

func prepareSystemSocket() (socketPath string, cleanup func() error, err error) {
	socketPath, err = xdg.ConfigFile(filepath.Join(fscmds.ServiceName, fscmds.ServerName))
	cleanup = func() error { return os.Remove(filepath.Dir(socketPath)) }
	return
}

var (
	SystemdScriptParameter = fscmds.CmdsParameterSet{
		Name:        "SystemdScript",
		Description: "Use custom systemd script",
		Environment: "FS_SERVICE_SYSTEMD",
	}
	UpstartScriptParameter = fscmds.CmdsParameterSet{
		Name:        "UpstartScript",
		Description: "Use custom upstart script",
		Environment: "FS_SERVICE_UPSTART",
	}
	SysvScriptParameter = fscmds.CmdsParameterSet{
		Name:        "SysvScript",
		Description: "Use custom sysv script",
		Environment: "FS_SERVICE_SYSV",
	}
	ReloadSignalParameter = fscmds.CmdsParameterSet{
		Name:        "ReloadSignal",
		Description: "Signal to send on reload.",
		Environment: "FS_SERVICE_RELOAD",
	}
	PidFileParameter = fscmds.CmdsParameterSet{
		Name:        "PIDFile",
		Description: "Location of the PID file.",
		Environment: "FS_SERVICE_PIDFILE",
	}
	RestartParameter = fscmds.CmdsParameterSet{
		Name:        "Restart",
		Description: "How the should be restarted.",
		Environment: "FS_SERVICE_RESTART",
	}
	SuccessExitStatusParameter = fscmds.CmdsParameterSet{
		Name:        "SuccessExitStatus",
		Description: "The list of exit status that shall be considered as successful, in addition to the default ones.",
		Environment: "FS_SERVICE_EXIT",
	}
	UserServiceParameter = fscmds.CmdsParameterSet{
		Name:        "UserService",
		Description: "Install as a current user service.",
		Environment: "FS_SERVICE_ASUSERS",
	}
	LogOutputParameter = fscmds.CmdsParameterSet{
		Name:        "LogOutput",
		Description: "Redirect StdErr & StandardOutPath to files.",
		Environment: "FS_SERVICE_LOG",
	}

	servicePosixOptions = platformOptions{
		stringOptions: []fscmds.CmdsParameterSet{
			SystemdScriptParameter,
			UpstartScriptParameter,
			SysvScriptParameter,
			ReloadSignalParameter,
			PidFileParameter,
			RestartParameter,
			SuccessExitStatusParameter,
		},
		boolOptions: []fscmds.CmdsParameterSet{
			LogOutputParameter,
			UserServiceParameter,
		},
	}
)
