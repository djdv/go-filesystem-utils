//go:build !nofuse

package cgofuse

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"runtime"
	"sync"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/u-root/uio/ulog"
	"github.com/winfsp/cgofuse/fuse"
)

type (
	errNo           = int
	fileDescriptor  = uint64
	fileType        = uint32
	filePermissions = uint32
	id              = uint32
	uid             = id
	gid             = id
	mountTable      map[string]*fuse.FileSystemHost
	Fuse            struct {
		mountsMu  sync.Mutex
		mounts    mountTable
		fsWrapper *goWrapper
		fuseHostSettings
	}
	fuseContext struct {
		uid
		gid
		// NOTE: PID omitted as not used.
	}
	fileHandle struct {
		ioMu   sync.Mutex // TODO: name and responsibility; currently applies to the position cursor
		goFile fs.File
	}
	seekerFile interface {
		fs.File
		io.Seeker
	}

	fillFunc = func(name string, stat *fuse.Stat_t, ofst int64) bool
)

const (
	posixRoot        = "/"
	operationSuccess = 0

	// SUSv4BSi7 permission bits
	// extended and aliased
	// for Go style conventions.

	executeOther = fuse.S_IXOTH
	writeOther   = fuse.S_IWOTH
	readOther    = fuse.S_IROTH

	executeGroup = fuse.S_IXGRP
	writeGroup   = fuse.S_IWGRP
	readGroup    = fuse.S_IRGRP

	executeUser = fuse.S_IXUSR
	writeUser   = fuse.S_IWUSR
	readUser    = fuse.S_IRUSR

	executeAll = executeUser | executeGroup | executeOther
	writeAll   = writeUser | writeGroup | writeOther
	readAll    = readUser | readGroup | readOther
)

func FSToFuse(fsys fs.FS, options ...WrapperOption) (*Fuse, error) {
	settings := wrapperSettings{
		log: ulog.Null,
		fuseHostSettings: fuseHostSettings{
			readdirPlus:     runtime.GOOS == "windows",
			deleteAccess:    false,
			caseInsensitive: false,
		},
	}
	if err := parseOptions(&settings, options...); err != nil {
		return nil, err
	}
	fuseSys := &Fuse{
		fuseHostSettings: settings.fuseHostSettings,
		fsWrapper: &goWrapper{
			FS:  fsys,
			log: settings.log,
		},
	}
	return fuseSys, nil
}

func (fh *Fuse) Mount(mountpoint string) error {
	fh.mountsMu.Lock()
	defer fh.mountsMu.Unlock()
	if fh.mountedLocked(mountpoint) {
		return fmt.Errorf(`"%s" is already in our mount table`, mountpoint)
	}
	var (
		fsID     filesystem.ID
		mounts   = fh.getMountsLocked()
		fsys     = fh.fsWrapper
		fuseHost = fuse.NewFileSystemHost(fsys)
	)
	if idFS, ok := fsys.FS.(filesystem.IDFS); ok {
		fsID = idFS.ID()
	}
	fh.fuseHostSettings.apply(fuseHost)

	target, args := makeFuseArgs(fsID, mountpoint)
	if err := attachToHost(fuseHost, target, args); err != nil {
		return err
	}
	mounts[mountpoint] = fuseHost
	return nil
}

func (fh *Fuse) mountedLocked(mountpoint string) bool {
	_, exists := fh.mounts[mountpoint]
	return exists
}

func (fh *Fuse) getMountsLocked() mountTable {
	mounts := fh.mounts
	if mounts == nil {
		mounts = make(mountTable)
		fh.mounts = mounts
	}
	return mounts
}

func (fh *Fuse) Unmount(mountpoint string) error {
	fh.mountsMu.Lock()
	defer fh.mountsMu.Unlock()
	return unmountLocked(fh.getMountsLocked(), mountpoint)
}

func unmountLocked(mounts mountTable, mountpoint string) error {
	fuseHost, ok := mounts[mountpoint]
	if !ok {
		return fmt.Errorf(`"%s" is not in our mount table`, mountpoint)
	}
	// NOTE: Since we can't disambiguate why a mountpoint failed to unmount,
	// we always delete it from the table. This lets the operator deal with it
	// at the host system level, and frees it from our system.
	// This is not ideal but out of our hands without a change to [Unmount].
	delete(mounts, mountpoint)
	if !fuseHost.Unmount() {
		return fmt.Errorf(`unmounting "%s" failed`+
			"- system log may have more information", mountpoint)
	}
	return nil
}

func (fh *Fuse) Close() (err error) {
	fh.mountsMu.Lock()
	defer fh.mountsMu.Unlock()
	mounts := fh.getMountsLocked()
	for mountpoint := range mounts {
		err = fserrors.Join(err, unmountLocked(mounts, mountpoint))
	}
	return err
}

func attachToHost(fuseSys *fuse.FileSystemHost, target string, args []string) error {
	// TODO (anyone): if there's a way to know mount has succeeded;
	// use that here.
	// Note that we can't just hook `Init` since that is called before
	// the code which actually does the mounting.
	// And we can't poll the mountpoint, since on most systems, for most targets,
	// it will already exist (but not be our mount).
	// As-is we can only assume mount succeeded if it doesn't
	// return an error after some arbitrary threshold.
	const deadlineDuration = 128 * time.Millisecond
	var (
		timer = time.NewTimer(deadlineDuration)
		errs  = make(chan error, 1)
	)
	defer timer.Stop()
	go func() {
		defer func() {
			// TODO: We should fork the lib so it errors
			// instead of panicking in this case.
			if r := recover(); r != nil {
				errs <- disambiguateCgoPanic(r)
			}
			close(errs)
		}()
		if !fuseSys.Mount(target, args) {
			err := fmt.Errorf(`failed to mount "%s" for an unknown reason`+
				"- system log may have more information", target)
			errs <- err
		}
	}()
	select {
	case err := <-errs:
		return err
	case <-timer.C:
		// `Mount` hasn't panicked or returned an error yet
		// assume `Mount` is blocking (as intended).
		return nil
	}
}

func disambiguateCgoPanic(r any) error {
	if panicString, ok := r.(string); ok &&
		panicString == cgoDepPanic {
		return errors.New(cgoDepMessage)
	}
	return fmt.Errorf("cgofuse panicked while attempting to mount: %v", r)
}
