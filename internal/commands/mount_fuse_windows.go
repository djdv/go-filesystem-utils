//go:build !nofuse

package commands

import (
	"fmt"
	"strings"
)

const (
	fuseHelpText = "Valid mount points may be:\n" +
		"- drive letters (`X:`)\n" +
		"- directory paths that do not refer to an existing file/directory (`X:\\mountpoint`)\n" +
		"- UNC locations (`\\\\Server\\Share`)\n"
	fuseExampleArgs = `M: C:\mountpoint \\localhost\mountpoint`
)

func fuseIDFlagText(kind string) (usageText, defaultText string) {
	usageText = fmt.Sprintf(
		"`%s` passed to WinFSP"+
			"\n(use WinFSP's `fsptool id` to obtain SID<->%s mappings)",
		kind,
		strings.ToUpper(kind),
	)
	defaultText = "caller of `mount`'s SID"
	return
}
