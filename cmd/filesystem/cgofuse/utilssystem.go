//+build !nofuse

package cgofuse

import (
	"context"
	"fmt"
	"io"
	gopath "path"
	"runtime"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
	"github.com/ipfs/go-ipfs/filesystem"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

type errNo = int

type fuseFillFunc func(name string, stat *fuselib.Stat_t, ofst int64) bool

type (
	// directoryPlus is used in `fillDir` to handle FUSE's readdir plus feature
	// (via a type assertion of objects returned from `UpgradeDirectory`).
	directoryPlus struct {
		filesystem.Directory
		statFunc
	}

	statFunc       func(name string) *fuselib.Stat_t
	readdirplusGen func(filesystem.Interface, string, *fuselib.Stat_t) statFunc
)

// upgradeDirectory binds a Directory and a means to get attributes for its elements.
// This should be used to transform directories into readdir plus capable directories
// before being sent to `fillDir`.
func upgradeDirectory(d filesystem.Directory, sf statFunc) filesystem.Directory {
	return directoryPlus{Directory: d, statFunc: sf}
}

func fillDir(ctx context.Context, directory filesystem.Directory, fill fuseFillFunc, offset int64) (int, error) {
	// TODO: uint <-> int shenanigans

	// Offset value 0 has a special meaning in FUSE (see: FUSE's `readdir` docs)
	// so all returned offsets values from us are expected to be 0>
	// `fillDir` expects the input directory to follow this convention, and supply us with offsets 0>

	var (
		statFunc statFunc
		stat     *fuselib.Stat_t
	)

	if dirPlus, ok := directory.(directoryPlus); ok {
		statFunc = dirPlus.statFunc
	} else {
		statFunc = func(string) *fuselib.Stat_t { return nil }
	}

	if offset == 0 {
		if err := directory.Reset(); err != nil {
			// NOTE: POSIX `rewinddir` is not expected to fail
			// if this happens, we'll inform FUSE's `readdir` that the stream position is (now) invalid
			return -fuselib.ENOENT, err // see: SUSv7 `readdir` "Errors"
		}
	}

	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	for ent := range directory.List(readCtx, uint64(offset)) {
		if err := ent.Error(); err != nil {
			return -fuselib.ENOENT, err
		}
		stat = statFunc(ent.Name())
		if !fill(ent.Name(), stat, int64(ent.Offset())) {
			break // fill asked us to stop filling
		}
	}

	return operationSuccess, nil
}

type statTimeGroup struct {
	atim, mtim, ctim, birthtim fuselib.Timespec
}

type statIDGroup struct {
	uid, gid uint32
	// These are omitted for now (as they're not used by us)
	// but belong in this structure if they become needed
	// Dev, Rdev uint64
}

func applyCommonsToStat(stat *fuselib.Stat_t, writable bool, tg statTimeGroup, ids statIDGroup) {
	stat.Atim, stat.Mtim, stat.Ctim, stat.Birthtim = tg.atim, tg.mtim, tg.ctim, tg.birthtim
	stat.Uid, stat.Gid = ids.uid, ids.gid

	if writable {
		stat.Mode |= IRWXA &^ (fuselib.S_IWOTH | fuselib.S_IXOTH) // |0774
	} else {
		stat.Mode |= IRXA &^ (fuselib.S_IXOTH) // |0554
	}
}

// TODO: inline
func releaseFile(table fileTable, handle uint64) (errNo, error) {
	file, err := table.Get(handle)
	if err != nil {
		return -fuselib.EBADF, err
	}

	// SUSv7 `close` (parphrased)
	// if errors are encountered, the result of the handle is unspecified
	// for us specifically, we'll remove the handle regardless of its close return

	if err := table.Remove(handle); err != nil {
		// TODO: if the error is not found we need to panic or return a severe error
		// this should not be possible
		return -fuselib.EBADF, err
	}

	return operationSuccess, file.Close()
}

// TODO: inline
func releaseDir(table directoryTable, handle uint64) (errNo, error) {
	dir, err := table.Get(handle)
	if err != nil {
		return -fuselib.EBADF, err
	}

	if err := table.Remove(handle); err != nil {
		// TODO: if the error is not found we need to panic or return a severe error
		// since that should not be possible
		return -fuselib.EBADF, err
	}

	// NOTE: even if close fails, we return system success
	// the relevant standard errors do not apply here; `releasedir` is not expected to fail
	// the handle was valid [!EBADF], and we didn't get interupted by the system [!EINTR]
	// if the returned error is not nil, it's an implementation fault that needs to be amended
	// in the directory interface implementation returned to us from the table
	return operationSuccess, dir.Close()
}

// TODO: read+write; we're not accounting for scenarios where the offset is beyond the end of the file
func readFile(file filesystem.File, buff []byte, ofst int64) (errNo, error) {
	if len(buff) == 0 {
		return 0, nil
	}

	if ofst < 0 {
		return -fuselib.EINVAL, fmt.Errorf("invalid offset %d", ofst)
	}

	if fileBound, err := file.Size(); err == nil {
		if ofst >= fileBound {
			return 0, nil // POSIX expects this
		}
	}

	if _, err := file.Seek(ofst, io.SeekStart); err != nil {
		return -fuselib.EIO, err
	}

	readBytes, err := file.Read(buff)
	if err != nil && err != io.EOF {
		readBytes = -fuselib.EIO // POSIX overloads this variable; at this point it becomes an error
	}

	// NOTE: we don't have to worry about `io.Reader` filling the segment beyond `buff[readBytes:]
	// (because of POSIX `read` semantics, the caller should not except bytes beyond `readBytes` to be valid)

	return readBytes, err // EOF will be returned if it was provided
}

func writeFile(file filesystem.File, buff []byte, ofst int64) (errNo, error) {
	if len(buff) == 0 {
		return 0, nil
	}

	if ofst < 0 {
		return -fuselib.EINVAL, fmt.Errorf("invalid offset %d", ofst)
	}

	/* TODO: test this; it should be handled internally by seek()+write()
	if not, uncomment, if so, remove

	if fileBound, err := file.Size(); err == nil {
		if ofst >= fileBound {
			newEnd := fileBound - (ofst - int64(len(buff)))
			if err := file.Truncate(uint64(newEnd)); err != nil { // pad 0's before our write
				return err, -fuselib.EIO
			}
		}
	}
	*/

	if _, err := file.Seek(ofst, io.SeekStart); err != nil {
		return -fuselib.EIO, fmt.Errorf("offset seek error: %s", err)
	}

	wroteBytes, err := file.Write(buff)
	if err != nil {
		return -fuselib.EIO, err
	}

	return wroteBytes, nil
}

func applyIntermediateStat(fStat *fuselib.Stat_t, iStat *filesystem.Stat) {
	// TODO [safety] we should probably panic if the uint64 source values exceed int64 positive range

	// retain existing permissions (if any), but reset the type bits
	fStat.Mode = (fStat.Mode &^ fuselib.S_IFMT) | coreTypeToFuseType(iStat.Type)

	// TODO: do this in the layer above
	// we need to know the type of what the link points to on Windows
	// FILE_ATTRIBUTE value on host is 0x410 for directory links, 0x420 for file links.
	// they'll work in operations, but not display properly in listings otherwise
	if runtime.GOOS == "windows" && iStat.Type == coreiface.TSymlink {
		// NOTE: for the sake of consistency with the native system
		// we ignore fields which are not set when calling NT's `CreateSymbolicLink` on an NTFS3.1 system
		fStat.Flags |= fuselib.UF_ARCHIVE // this is set by the system native, so we'll emulate that
		// no other fields we have access to are significant to NT here
		return
	}

	fStat.Size = int64(iStat.Size)
	fStat.Blksize = int64(iStat.BlockSize)
	fStat.Blocks = int64(iStat.Blocks)
}

type fuseFileType = uint32

func coreTypeToFuseType(ct coreiface.FileType) fuseFileType {
	switch ct {
	case coreiface.TDirectory:
		return fuselib.S_IFDIR
	case coreiface.TSymlink:
		return fuselib.S_IFLNK
	case coreiface.TFile:
		return fuselib.S_IFREG
	default:
		return 0
	}
}

func ioFlagsFromFuse(fuseFlags int) filesystem.IOFlags {
	switch fuseFlags & fuselib.O_ACCMODE {
	case fuselib.O_RDONLY:
		return filesystem.IOReadOnly
	case fuselib.O_WRONLY:
		return filesystem.IOWriteOnly
	case fuselib.O_RDWR:
		return filesystem.IOReadWrite
	default:
		return filesystem.IOFlags(0)
	}
}

func getStat(r filesystem.Interface, path string, template *fuselib.Stat_t) *fuselib.Stat_t {
	iStat, _, err := r.Info(path, filesystem.StatRequestAll)
	if err != nil {
		return nil
	}

	subStat := new(fuselib.Stat_t)
	*subStat = *template
	applyIntermediateStat(subStat, iStat)
	return subStat
}

// statticStat generates a statFunc
// that fetches attributes for a requests, and caches the results for subsequent requests
func staticStat(r filesystem.Interface, basePath string, template *fuselib.Stat_t) statFunc {
	stats := make(map[string]*fuselib.Stat_t, 1)

	return func(name string) *fuselib.Stat_t {
		if cachedStat, ok := stats[name]; ok {
			return cachedStat
		}

		subStat := getStat(r, gopath.Join(basePath, name), template)
		stats[name] = subStat
		return subStat
	}
}

// dynamicStat generates a statFunc
// that always fetches attributes for a requests (assuming they may have changed since the last request)
func dynamicStat(r filesystem.Interface, basePath string, template *fuselib.Stat_t) statFunc {
	return func(name string) *fuselib.Stat_t {
		return getStat(r, gopath.Join(basePath, name), template)
	}
}
