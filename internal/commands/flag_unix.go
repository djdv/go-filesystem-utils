//go:build unix

package commands

import (
	"io/fs"
	"syscall"
)

func getUmask() fs.FileMode {
	mask := syscall.Umask(0)
	syscall.Umask(mask)
	return fs.FileMode(mask)
}
