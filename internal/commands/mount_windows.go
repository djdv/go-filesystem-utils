package commands

import (
	"flag"
	"fmt"
	"strings"
)

func bindFUSEIDFlag(name, fuseName string,
	reference *uint32, flagSet *flag.FlagSet,
	setMonitor *bool,
) {
	const (
		usageFmt = "`%s` passed to WinFSP" +
			"\n(use WinFSP's `fsptool id` to obtain SID<->%s mappings)"
		defaultText = "caller of `mount`'s SID"
	)
	*reference = ^uint32(0)
	flagSet.Func(name,
		fmt.Sprintf(usageFmt,
			fuseName, strings.ToUpper(fuseName),
		),
		func(s string) error {
			id, err := parseID[uint32](s)
			if err != nil {
				return err
			}
			*reference = id
			*setMonitor = true
			return nil
		})
	setDefaultValueText(flagSet, flagDefaultText{
		name: defaultText,
	})
}
