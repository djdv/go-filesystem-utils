//go:build !nofuse && !windows

package commands

import (
	"flag"
	"fmt"
)

func bindFUSEIDFlag(name, fuseName string,
	reference *uint32, flagSet *flag.FlagSet,
	setMonitor *bool,
) {
	const usageFmt = "`%s` passed to FUSE"
	flagSet.Func(name,
		fmt.Sprintf(usageFmt, fuseName),
		func(s string) error {
			id, err := parseID[uint32](s)
			if err != nil {
				return err
			}
			*reference = id
			*setMonitor = true
			return nil
		})
}
