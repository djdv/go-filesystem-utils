package client

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

func procRunning(proc *os.Process) bool {
	// NOTE: proc already contains a handle, but it's unexported.
	// We could ask the runtime to let us read it, but this is safer.
	const (
		STILL_ACTIVE  = windows.STATUS_PENDING
		desiredAccess = windows.PROCESS_QUERY_LIMITED_INFORMATION
		inheritHandle = false
	)
	pid := uint32(proc.Pid)
	handle, err := windows.OpenProcess(desiredAccess, inheritHandle, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var exitcode uint32
	if windows.GetExitCodeProcess(handle, &exitcode) != nil {
		return false
	}
	return windows.NTStatus(exitcode) == STILL_ACTIVE
}

func childProcInit() { /* NOOP */ }

func emancipatedSubproc() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow: true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP |
			windows.DETACHED_PROCESS,
	}
}
