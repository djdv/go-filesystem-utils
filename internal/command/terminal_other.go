//go:build !windows

package command

import (
	"io"
	"os"
)

func ansiStderr() io.Writer { return os.Stderr }
