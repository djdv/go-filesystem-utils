package p9

import (
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

	jsonMap = map[string]any
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
	var (
		errs         = make(chan error)
		apiDirs, err = gatherMountAPIs(mounts, errs)
	)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	for apiDir := range apiDirs {
		wg.Add(1)
		go func(api p9.File) {
			unlinkAllChildren(api, errs)
			if cErr := api.Close(); cErr != nil {
				errs <- cErr
			}
			wg.Done()
		}(apiDir)
	}
	go func() { wg.Wait(); close(errs) }()
	for e := range errs {
		err = fserrors.Join(err, e)
	}
	return err
}

func UnmountTargets(mounts p9.File, mountPoints []string) error {
	var (
		errs         = make(chan error)
		apiDirs, err = gatherMountAPIs(mounts, errs)
	)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	unlinked := make([]string, 0, len(mountPoints))
	for apiDir := range apiDirs {
		wg.Add(1)
		go func(api p9.File) {
			removed, rErr := unlinkMountFiles(api, mountPoints, errs)
			if rErr == nil {
				for target := range removed {
					unlinked = append(unlinked, target)
				}
			} else {
				errs <- rErr
			}
			if cErr := api.Close(); cErr != nil {
				errs <- cErr
			}
			wg.Done()
		}(apiDir)
	}
	go func() { wg.Wait(); close(errs) }()
	for e := range errs {
		err = fserrors.Join(err, e)
	}
	if len(unlinked) != len(mountPoints) {
		err = formatUnmountOperationErr(mountPoints, unlinked, err)
	}
	return err
}

func formatUnmountOperationErr(mountPoints, unlinked []string, err error) error {
	var (
		remaining = make([]string, len(mountPoints))
		_         = copy(remaining, mountPoints)
		remove    = func(target string) {
			for i, existing := range remaining {
				if existing == target {
					remaining[i] = remaining[len(remaining)-1]
					remaining = remaining[:len(remaining)-1]
					break
				}
			}
		}
	)
	for _, removed := range unlinked {
		remove(removed)
	}
	if err != nil {
		if joinErrs, ok := err.(interface {
			Unwrap() []error
		}); ok {
			var ue unmountError
			for _, e := range joinErrs.Unwrap() {
				if errors.As(e, &ue) {
					remove(ue.target)
				}
			}
		}
	}
	for i, path := range remaining {
		remaining[i] = fmt.Sprintf(`"%s"`, path)
	}
	const prefix = "could not find mount point"
	var errStr string
	if len(remaining) == 1 {
		errStr = fmt.Sprintf(prefix+": %s", remaining[0])
	} else {
		errStr = fmt.Sprintf(prefix+"s: %s", strings.Join(remaining, ", "))
	}
	return fserrors.Join(err, errors.New(errStr))
}

func gatherMountAPIs(mounts p9.File, errs chan<- error) (<-chan p9.File, error) {
	hostDirs, err := gatherEnts(mounts, errs)
	if err != nil {
		return nil, err
	}
	apiRelay := make(chan p9.File)
	go func() {
		var wg sync.WaitGroup
		for hostDir := range hostDirs {
			apis, err := gatherEnts(hostDir, errs)
			if err != nil {
				errs <- err
				continue
			}
			wg.Add(1)
			go func(host p9.File) {
				for api := range apis {
					apiRelay <- api
				}
				if err := host.Close(); err != nil {
					errs <- err
				}
				wg.Done()
			}(hostDir)
		}
		wg.Wait()
		close(apiRelay)
	}()
	return apiRelay, nil
}

func unlinkMountFiles(apiDir p9.File, mountPoints []string, errs chan<- error) (<-chan string, error) {
	mountEnts, err := ReadDir(apiDir)
	if err != nil {
		return nil, err
	}
	var (
		wg        sync.WaitGroup
		remaining = make([]string, len(mountPoints))
		_         = copy(remaining, mountPoints)
		removed   = make(chan string, len(mountPoints))
	)
	wg.Add(len(mountEnts))
	for _, mountEnt := range mountEnts {
		go func(ent p9.Dirent) {
			defer wg.Done()
			fileMap, err := getMountData(apiDir, ent)
			if err != nil {
				errs <- err
				return
			}
			for i, target := range remaining {
				hasTarget, err := hasTarget(fileMap, target)
				if err != nil {
					errs <- err
					continue
				}
				if !hasTarget {
					continue
				}
				remaining[i] = remaining[len(remaining)-1]
				remaining = remaining[:len(remaining)-1]
				const flags = 0
				if err := apiDir.UnlinkAt(ent.Name, flags); err == nil {
					removed <- target
				} else {
					errs <- unmountError{
						error:  err,
						target: target,
					}
				}
				break
			}
		}(mountEnt)
	}
	go func() { wg.Wait(); close(removed) }()
	return removed, nil
}

func getMountData(apiDir p9.File, ent p9.Dirent) (jsonMap, error) {
	mountFile, err := walkEnt(apiDir, ent)
	if err != nil {
		return nil, err
	}
	fileData, err := ReadAll(mountFile)
	if err != nil {
		return nil, err
	}
	if err := mountFile.Close(); err != nil {
		return nil, err
	}
	var fileMap jsonMap
	if err := json.Unmarshal(fileData, &fileMap); err != nil {
		return nil, err
	}
	return fileMap, nil
}

// TODO:
// consider taking in a list of fields to check
// this way the client program
// can have some insight+influence
// on which fields are likely the target fields
// this can even be an inverse of the file map.
// i.e. {target:field}
// so for each target, we know what field it should be in.
// ^ this won't work as-is
// client command doesn't
// remember what API the target was mounted with
// 1D list of fields is still better than all fields.
// host packages can expose their target field
// as some const, like is done with fsys.Host, fsys.ID
func hasTarget(fileData jsonMap, target string) (bool, error) {
	for _, value := range fileData {
		if object, ok := value.(jsonMap); ok {
			found, err := hasTarget(object, target)
			if err != nil {
				return false, err
			}
			if found {
				return true, nil
			}
			continue
		}
		if jString, ok := value.(string); ok {
			if jString == target {
				return true, nil
			}
		}
	}
	return false, nil
}
