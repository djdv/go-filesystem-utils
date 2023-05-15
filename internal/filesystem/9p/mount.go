package p9

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

type (
	MountFile struct {
		directory
		makeHostFn MakeHostFunc
	}
	// TODO: Concern:
	// clients could return a file when we want a directory
	// it'd be possible to tell the client which 9P op
	// was called and allow both.
	// Internally we can stat the returned file to make sure
	// it complies.
	// i.e. add `operation{mknod|mkdir}` parameter to this,
	// and when doing the operation, call GetAttr on the returned file
	// (to validate {regular|directory}).
	//
	// MakeHostFunc should handle file creation operations
	// for files representing a [filesystem.Host].
	MakeHostFunc func(parent p9.File, host filesystem.Host,
		permissions p9.FileMode,
		uid p9.UID, gid p9.GID) (p9.QID, p9.File, error)
	mounterSettings struct {
		directoryOptions []DirectoryOption
	}
	MounterOption func(*mounterSettings) error

	unmountError struct {
		error
		target string
	}
	// DecodeTargetFunc will be called with bytes representing
	// an encoded mount point, and should decode then return
	// the mount point's target.
	// Under typical operation, the encoded data should
	// have the same format as the argument passed to
	// [Client.Mount]. However, this is not guaranteed;
	// as different clients with different formats may
	// call `Mount` and `Unmount` independently.
	DecodeTargetFunc func(filesystem.Host, filesystem.ID, []byte) (string, error)
)

func (ue unmountError) Error() string {
	return fmt.Sprintf(
		"could not remove: \"%s\" - %s",
		ue.target, ue.error,
	)
}

func NewMounter(makeHostFn MakeHostFunc, options ...MounterOption) (p9.QID, *MountFile, error) {
	var settings mounterSettings
	if err := parseOptions(&settings, options...); err != nil {
		return p9.QID{}, nil, err
	}
	qid, directory, err := NewDirectory(settings.directoryOptions...)
	if err != nil {
		return p9.QID{}, nil, err
	}
	return qid, &MountFile{
		directory:  directory,
		makeHostFn: makeHostFn,
	}, nil
}

func (mf *MountFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	qids, file, err := mf.directory.Walk(names)
	if len(names) == 0 {
		file = &MountFile{
			directory:  file,
			makeHostFn: mf.makeHostFn,
		}
	}
	return qids, file, err
}

func (mf *MountFile) Mkdir(name string, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, error) {
	uid, gid, err := mkPreamble(mf, name, uid, gid)
	if err != nil {
		return p9.QID{}, err
	}
	qid, file, err := mf.makeHostFn(mf, filesystem.Host(name),
		permissions, uid, gid)
	if err != nil {
		return p9.QID{}, fserrors.Join(perrors.EACCES, err)
	}
	return qid, mf.Link(file, name)
}

func UnmountAll(mounts p9.File) error {
	return UnmountTargets(mounts, nil, nil)
}

func UnmountTargets(mounts p9.File,
	mountPoints []string, decodeTargetFn DecodeTargetFunc,
) error {
	var (
		errs        []error
		unlinked    = make([]string, 0, len(mountPoints))
		ctx, cancel = context.WithCancel(context.Background())
		results     = unmountTargets(ctx, mounts,
			mountPoints, decodeTargetFn)
	)
	defer cancel()
	for result := range results {
		if err := result.error; err != nil {
			errs = append(errs, err)
			continue
		}
		unlinked = append(unlinked, result.value)
	}
	if len(mountPoints) != len(unlinked) ||
		errs != nil {
		return formatUnmountErr(mountPoints, unlinked, errs)
	}
	return nil
}

func unmountTargets(ctx context.Context,
	mounts p9.File, mountPoints []string,
	decodeTargetFn DecodeTargetFunc,
) <-chan stringResult {
	return mapDirPipeline(ctx, mounts,
		func(ctx context.Context, dir p9.File,
			wg *sync.WaitGroup, results chan<- stringResult,
		) {
			unmountTargetsPipeline(ctx, dir,
				mountPoints, decodeTargetFn,
				wg, results,
			)
		})
}

func unmountTargetsPipeline(ctx context.Context,
	mounts p9.File, mountPoints []string, decodeTargetFn DecodeTargetFunc,
	wg *sync.WaitGroup, results chan<- stringResult,
) {
	defer wg.Done()
	unmountAll := mountPoints == nil
	checkErr := func(err error) (sawError bool) {
		if sawError = err != nil; sawError {
			sendResult(ctx, results, stringResult{error: err})
		}
		return sawError
	}
	processEntry := func(result direntResult, dir p9.File, dirWg *sync.WaitGroup) {
		defer dirWg.Done()
		if checkErr(result.error) {
			return
		}
		entry := result.value
		const unlinkFlags = 0
		if unmountAll {
			checkErr(dir.UnlinkAt(entry.Name, unlinkFlags))
			return
		}
		unmountGuestEntry(ctx,
			dir, entry,
			mountPoints, decodeTargetFn,
			results,
		)
	}
	processGuest := func(result fileResult) {
		defer wg.Done()
		if checkErr(result.error) {
			return
		}
		var (
			dirWg        sync.WaitGroup
			guestDir     = result.value
			guestResults = getDirents(ctx, guestDir)
		)
		for result := range guestResults {
			dirWg.Add(1)
			go processEntry(result, guestDir, &dirWg)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			dirWg.Wait()
			checkErr(guestDir.Close())
		}()
	}
	for result := range flattenMounts(ctx, mounts) {
		wg.Add(1)
		go processGuest(result)
	}
}

// flattenMounts returns all guest directories
// for all hosts within mounts.
func flattenMounts(ctx context.Context, mounts p9.File) <-chan fileResult {
	return mapDirPipeline(ctx, mounts, flattenMountsPipeline)
}

func flattenMountsPipeline(ctx context.Context, mounts p9.File,
	wg *sync.WaitGroup, results chan<- fileResult,
) {
	defer wg.Done()
	processHost := func(result fileResult) {
		defer wg.Done()
		if err := result.error; err != nil {
			sendResult(ctx, results, fileResult{error: err})
			return
		}
		var (
			hostDir     = result.value
			hostResults = getDirFiles(ctx, hostDir)
		)
		if err := hostDir.Close(); err != nil {
			sendResult(ctx, results, fileResult{error: err})
		}
		for result := range hostResults {
			wg.Add(1)
			go func(res fileResult) {
				defer wg.Done()
				sendResult(ctx, results, res)
			}(result)
		}
	}
	for result := range getDirFiles(ctx, mounts) {
		wg.Add(1)
		go processHost(result)
	}
}

func unmountGuestEntry(ctx context.Context,
	dir p9.File, entry p9.Dirent,
	mountPoints []string, decodeTargetFn DecodeTargetFunc,
	results chan<- stringResult,
) {
	mountFile, err := walkEnt(dir, entry)
	if err != nil {
		sendResult(ctx, results, stringResult{error: err})
		return
	}
	defer func() {
		if err := mountFile.Close(); err != nil {
			sendResult(ctx, results, stringResult{error: err})
		}
	}()
	target, err := parseMountFile(mountFile, decodeTargetFn)
	if err != nil {
		sendResult(ctx, results, stringResult{error: err})
		return
	}
	for _, point := range mountPoints {
		if point != target {
			continue
		}
		const unlinkFlags = 0
		err := dir.UnlinkAt(entry.Name, unlinkFlags)
		if err != nil {
			err = unmountError{target: target, error: err}
		}
		sendResult(ctx, results, stringResult{value: target, error: err})
		return
	}
}

func parseMountFile(file p9.File, decodeFn DecodeTargetFunc) (string, error) {
	fileData, err := ReadAll(file)
	if err != nil {
		return "", err
	}
	var point mountPointMarshal
	if err := json.Unmarshal(fileData, &point); err != nil {
		return "", err
	}
	return decodeFn(point.Host, point.ID, point.Data)
}

func formatUnmountErr(mountPoints, unlinked []string, errs []error) error {
	faulty := make([]string, 0, len(errs))
	for _, err := range errs {
		var uErr unmountError
		if errors.As(err, &uErr) {
			faulty = append(faulty, uErr.target)
		}
	}
	var (
		skip      = append(faulty, unlinked...)
		remaining = make([]string, 0, len(mountPoints)-len(skip))
	)
reduce:
	for _, target := range mountPoints {
		for _, skipped := range skip {
			if target == skipped {
				continue reduce
			}
		}
		remaining = append(remaining, fmt.Sprintf(`"%s"`, target))
	}
	const prefix = "could not find mount point"
	var errStr string
	if len(remaining) == 1 {
		errStr = fmt.Sprintf(prefix+": %s", remaining[0])
	} else {
		errStr = fmt.Sprintf(prefix+"s: %s", strings.Join(remaining, ", "))
	}
	return fserrors.Join(append(errs, errors.New(errStr))...)
}
