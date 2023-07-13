package cgofuse

import (
	"path/filepath"
	"unsafe"

	"github.com/winfsp/cgofuse/fuse"
	"golang.org/x/sys/windows"
)

const LOAD_LIBRARY_SEARCH_SYSTEM32 = 0x00000800

// TODO review

func loadSystemDLL(name string) (*windows.DLL, error) {
	modHandle, err := windows.LoadLibraryEx(name, 0, LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		return nil, err
	}
	return &windows.DLL{Name: name, Handle: modHandle}, nil
}

func statfs(path string, fStatfs *fuse.Statfs_t) (int, error) {
	mod, err := loadSystemDLL("kernel32.dll")
	if err != nil {
		return -fuse.ENOMEM, err // kind of true, probably better than EIO
	}
	defer mod.Release()

	proc, err := mod.FindProc("GetDiskFreeSpaceExW")
	if err != nil {
		return -fuse.ENOMEM, err // kind of true, probably better than EIO
	}

	var (
		FreeBytesAvailableToCaller,
		TotalNumberOfBytes,
		TotalNumberOfFreeBytes uint64

		SectorsPerCluster,
		BytesPerSector uint16
		// NumberOfFreeClusters,
		// TotalNumberOfClusters uint16
	)
	// HACK:
	// Explorer gets upset when we try to write files
	// to a mountpoint that has 0 free space.
	//
	// We need some way to place a host path
	// on the fuse system, that we can use as a reference
	// For IPFS, this can be the volume the node is on
	// when it's remote this can be the system volume / flag overrrideable.
	// We need some option for "infinite".
	//
	// better still would be a decimal value of "free space"
	// which we can maintain during calls to write, rm, etc.
	// the initial value could be derived from the above sources
	// before being pased to the fuse interface.
	sysdrive, err := windows.GetWindowsDirectory()
	if err != nil {
		return -fuse.EIO, err
	}
	path = sysdrive
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return -fuse.EFAULT, err // caller should check for syscall.EINVAL; NUL byte was in string
	}

	r1, _, wErr := proc.Call(uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&FreeBytesAvailableToCaller)),
		uintptr(unsafe.Pointer(&TotalNumberOfBytes)),
		uintptr(unsafe.Pointer(&TotalNumberOfFreeBytes)),
	)
	if r1 == 0 {
		return -fuse.ENOMEM, wErr
	}

	proc, _ = mod.FindProc("GetDiskFreeSpaceW")
	r1, _, wErr = proc.Call(uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&SectorsPerCluster)),
		uintptr(unsafe.Pointer(&BytesPerSector)),
		// uintptr(unsafe.Pointer(&NumberOfFreeClusters)),
		0,
		// uintptr(unsafe.Pointer(&TotalNumberOfClusters)),
		0,
	)
	if r1 == 0 {
		return -fuse.EIO, wErr
	}

	var (
		componentLimit = new(uint32)
		volumeFlags    = new(uint32)
		volumeSerial   = new(uint32)
	)

	volumeRoot := filepath.VolumeName(path) + string(filepath.Separator)
	pathPtr, err = windows.UTF16PtrFromString(volumeRoot)
	if err != nil {
		return -fuse.EFAULT, err // caller should check for syscall.EINVAL; NUL byte was in string
	}

	if err = windows.GetVolumeInformation(pathPtr, nil, 0, volumeSerial, componentLimit, volumeFlags, nil, 0); err != nil {
		return -fuse.EIO, err
	}

	fStatfs.Bsize = uint64(SectorsPerCluster * BytesPerSector)
	fStatfs.Frsize = uint64(BytesPerSector)
	fStatfs.Blocks = TotalNumberOfBytes / uint64(BytesPerSector)
	fStatfs.Bfree = TotalNumberOfFreeBytes / (uint64(BytesPerSector))
	fStatfs.Bavail = FreeBytesAvailableToCaller / (uint64(BytesPerSector))
	fStatfs.Files = ^uint64(0)

	// TODO: these have to come from our own file table
	// fStatfs.Ffree = nodeBinding.AvailableHandles()
	// fStatfs.Favail = fStatfs.Ffree

	fStatfs.Namemax = uint64(*componentLimit)

	// cgofuse ignores these but we have them
	fStatfs.Flag = uint64(*volumeFlags)
	fStatfs.Fsid = uint64(*volumeSerial)

	return operationSuccess, nil
}
