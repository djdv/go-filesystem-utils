package commands

import "flag"

func bindServiceControlFlags(flagSet *flag.FlagSet, options *controlOptions) {
	// NOTE: Key+value types defined on
	// [service.KeyValue] documentation.
	bindServiceControlFlagsPOSIX(flagSet, options)
	const (
		prefixName  = serviceFlagPrefix + "prefix"
		prefixUsage = "service FMRI prefix"
	)
	flagSetFunc(flagSet, prefixName, prefixUsage, options,
		func(value string, settings *controlSettings) error {
			settings.Config.Option["Prefix"] = value
			return nil
		})
}
