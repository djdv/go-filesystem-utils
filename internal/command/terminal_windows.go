package command

import (
	"io"
	"os"

	"github.com/mattn/go-colorable"
	"golang.org/x/sys/windows"
)

const vt100Mode = windows.ENABLE_PROCESSED_OUTPUT |
	windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING

func ansiStderr() io.Writer {
	var (
		hStderr   = windows.Stderr
		mode, err = getConsoleMode(hStderr)
	)
	if err != nil {
		return colorable.NewNonColorable(os.Stderr)
	}
	if !hasVirtualTerminalProcessing(mode) {
		if err := enableVirtualTerminalProcessing(hStderr, mode); err != nil {
			return colorable.NewColorable(os.Stderr)
		}
	}
	return os.Stderr
}

func getConsoleMode(handle windows.Handle) (mode uint32, err error) {
	err = windows.GetConsoleMode(handle, &mode)
	return
}

func hasVirtualTerminalProcessing(mode uint32) bool {
	return mode&vt100Mode == vt100Mode
}

func enableVirtualTerminalProcessing(handle windows.Handle, mode uint32) error {
	return windows.SetConsoleMode(handle, mode|vt100Mode)
}
