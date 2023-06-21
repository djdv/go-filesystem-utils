//go:build !nofuse && !windows

package commands

import (
	"fmt"
	"strings"
)

const (
	fuseHelpText = "Valid mount points may be:\n" +
		"- directory paths that refer to an existing directory (`/mnt/mountpoint`)\n"
	fuseExampleArgs    = `/mnt/ipfs`
	fuseUIDDefault     = 0
	fuseGIDDefault     = 0
	readdirPlusCapible = false
)

func fuseIDFlagText(kind string) (usageText, defaultText string) {
	usageText = fmt.Sprintf(
		"`%s` passed to FUSE"+
			kind,
		strings.ToUpper(kind),
	)
	return
}
