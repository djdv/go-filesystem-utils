package commands

import "flag"

func bindServiceControlFlags(flagSet *flag.FlagSet, options *controlOptions) {
	// NOTE: Key+value types defined on
	// [service.KeyValue] documentation.
	bindServiceControlFlagsPOSIX(flagSet, options)
	const (
		launchdConfigName  = serviceFlagPrefix + "launchd-config"
		launchdConfigUsage = "use custom launchd config"
	)
	flagSetFunc(flagSet, launchdConfigName, launchdConfigUsage, options,
		func(value string, settings *controlSettings) error {
			settings.Config.Option["LaunchdConfig"] = value
			return nil
		})
	const (
		keepAliveName  = serviceFlagPrefix + "keep-alive"
		keepAliveUsage = "prevent the system from stopping the service automatically"
	)
	flagSetFunc(flagSet, keepAliveName, keepAliveUsage, options,
		func(value bool, settings *controlSettings) error {
			settings.Config.Option["KeepAlive"] = value
			return nil
		})
	const (
		runAtLoadName  = serviceFlagPrefix + "run-at-load"
		runAtLoadUsage = "run the service after its job has been loaded"
	)
	flagSetFunc(flagSet, runAtLoadName, runAtLoadUsage, options,
		func(value bool, settings *controlSettings) error {
			settings.Config.Option["RunAtLoad"] = value
			return nil
		})
	const (
		sessionCreateName  = serviceFlagPrefix + "session-create"
		sessionCreateUsage = "create a full user session"
	)
	flagSetFunc(flagSet, sessionCreateName, sessionCreateUsage, options,
		func(value bool, settings *controlSettings) error {
			settings.Config.Option["SessionCreate"] = value
			return nil
		})
	const (
		socketsTypeName  = serviceFlagPrefix + "sock-type"
		socketsTypeUsage = `what type of socket to create ("stream", "dgram", "seqpacket")`
	)
	flagSetFunc(flagSet, socketsTypeName, socketsTypeUsage, options,
		func(value string, settings *controlSettings) error {
			settings.Config.Option["SockType"] = value
			return nil
		})
	const (
		socketsPathNameName  = serviceFlagPrefix + "sockets-path-name"
		socketsPathNameUsage = "Unix domain socket path"
	)
	flagSetFunc(flagSet, socketsPathNameName, socketsPathNameUsage, options,
		func(value string, settings *controlSettings) error {
			settings.Config.Option["SockPathName"] = value
			return nil
		})
	const (
		socketsPathModeName  = serviceFlagPrefix + "sockets-path-mode"
		socketsPathModeUsage = "socket file mode bits (must be decimal; not octal)"
	)
	flagSetFunc(flagSet, socketsPathModeName, socketsPathModeUsage, options,
		func(value int, settings *controlSettings) error {
			settings.Config.Option["SockPathMode"] = value
			return nil
		})
}
