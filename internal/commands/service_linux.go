package commands

import "flag"

func bindServiceControlFlags(flagSet *flag.FlagSet, options *controlOptions) {
	bindServiceControlFlagsPOSIX(flagSet, options)
	const (
		openRCScriptName  = serviceFlagPrefix + "openrc-script"
		openRCScriptUsage = "use custom OpenRC script"
	)
	flagSetFunc(flagSet, openRCScriptName, openRCScriptUsage, options,
		func(value string, settings *controlSettings) error {
			settings.Config.Option["OpenRCScript"] = value
			return nil
		})
	const (
		limitNOFILEName  = serviceFlagPrefix + "limit-nofile"
		limitNOFILEUsage = "maximum open files"
	)
	flagSetFunc(flagSet, limitNOFILEName, limitNOFILEUsage, options,
		func(value int, settings *controlSettings) error {
			settings.Config.Option["LimitNOFILE"] = value
			return nil
		})
}
