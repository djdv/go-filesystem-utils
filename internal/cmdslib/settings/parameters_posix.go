//go:build !windows
// +build !windows

package settings

import "github.com/djdv/go-filesystem-utils/internal/parameters"

type POSIXSettings struct {
	UserService bool
	SystemdScript,
	UpstartScript,
	SysvScript,
	// RunWait omitted
	ReloadSignal,
	PIDFile string
	LogOutput bool
	Restart,
	SuccessExitStatus string
}

func (*POSIXSettings) Parameters() parameters.Parameters {
	return parameters.Parameters{
		UserService(),
		SystemdScript(),
		UpstartScript(),
		SysvScript(),
		// RunWait omitted
		ReloadSignal(),
		PIDFile(),
		LogOutput(),
		Restart(),
		SuccessExitStatus(),
	}
}

func SystemdScript() parameters.Parameter {
	return parameters.NewParameter(
		"Use custom systemd script",
	)
}

func UpstartScript() parameters.Parameter {
	return parameters.NewParameter(
		"Use custom upstart script",
	)
}

func SysvScript() parameters.Parameter {
	return parameters.NewParameter(
		"Use custom sysv script",
	)
}

func ReloadSignal() parameters.Parameter {
	return parameters.NewParameter(
		"Signal to send on reload.",
	)
}

func PIDFile() parameters.Parameter {
	return parameters.NewParameter(
		"Location of the PID file.",
	)
}

func Restart() parameters.Parameter {
	return parameters.NewParameter(
		"How the should be restarted.",
	)
}

func SuccessExitStatus() parameters.Parameter {
	return parameters.NewParameter(
		"The list of exit status that shall be considered as successful, in addition to the default ones.",
	)
}

func UserService() parameters.Parameter {
	return parameters.NewParameter(
		"Install as a current user service.",
	)
}

func LogOutput() parameters.Parameter {
	return parameters.NewParameter(
		"Redirect StdErr & StandardOutPath to files.",
	)
}
