//go:build !windows

package commands

import "flag"

func bindServiceControlFlagsPOSIX(flagSet *flag.FlagSet, options *controlOptions) {
	// NOTE: Key+value types defined on
	// [service.KeyValue] documentation.
	const (
		userServiceName  = serviceFlagPrefix + "user-service"
		userServiceUsage = "install as a current user service"
	)
	flagSetFunc(flagSet, userServiceName, userServiceUsage, options,
		func(value bool, settings *controlSettings) error {
			settings.Config.Option["UserService"] = value
			return nil
		})
	const (
		systemdScriptName  = serviceFlagPrefix + "systemd-script"
		systemdScriptUsage = "use custom systemd script"
	)
	flagSetFunc(flagSet, systemdScriptName, systemdScriptUsage, options,
		func(value string, settings *controlSettings) error {
			settings.Config.Option["SystemdScript"] = value
			return nil
		})
	const (
		upstartScriptName  = serviceFlagPrefix + "upstart-script"
		upstartScriptUsage = "use custom upstart script"
	)
	flagSetFunc(flagSet, upstartScriptName, upstartScriptUsage, options,
		func(value string, settings *controlSettings) error {
			settings.Config.Option["UpstartScript"] = value
			return nil
		})
	const (
		sysvScriptName  = serviceFlagPrefix + "sysv-script"
		sysvScriptUsage = "use custom sysv script"
	)
	flagSetFunc(flagSet, sysvScriptName, sysvScriptUsage, options,
		func(value string, settings *controlSettings) error {
			settings.Config.Option["SysvScript"] = value
			return nil
		})
	const (
		reloadSignalName  = serviceFlagPrefix + "reload-signal"
		reloadSignalUsage = "signal to send on reload"
	)
	flagSetFunc(flagSet, reloadSignalName, reloadSignalUsage, options,
		func(value string, settings *controlSettings) error {
			settings.Config.Option["ReloadSignal"] = value
			return nil
		})
	const (
		pidFileName  = serviceFlagPrefix + "pid-file"
		pidFileUsage = "location of the PID file"
	)
	flagSetFunc(flagSet, pidFileName, pidFileUsage, options,
		func(value string, settings *controlSettings) error {
			settings.Config.Option["PIDFile"] = value
			return nil
		})
	const (
		logOutputName  = serviceFlagPrefix + "log-output"
		logOutputUsage = "redirect StdErr & StandardOutPath to files"
	)
	flagSetFunc(flagSet, logOutputName, logOutputUsage, options,
		func(value bool, settings *controlSettings) error {
			settings.Config.Option["LogOutput"] = value
			return nil
		})
	const (
		restartName  = serviceFlagPrefix + "restart"
		restartUsage = "service restart keyword (e.g. \"always\")"
	)
	flagSetFunc(flagSet, restartName, restartUsage, options,
		func(value string, settings *controlSettings) error {
			settings.Config.Option["Restart"] = value
			return nil
		})
	const (
		successExitStatusName  = serviceFlagPrefix + "success-exit-status"
		successExitStatusUsage = "the list of exit status that shall be considered as successful, in addition to the default ones"
	)
	flagSetFunc(flagSet, successExitStatusName, successExitStatusUsage, options,
		func(value string, settings *controlSettings) error {
			settings.Config.Option["SuccessExitStatus"] = value
			return nil
		})
}
