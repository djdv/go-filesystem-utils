package cgofuse

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/jaevor/go-nanoid"
	"github.com/u-root/uio/ulog"
	"github.com/winfsp/cgofuse/fuse"
)

const (
	// syscallFailedFmt is used for both the `mount` and `unmount`
	// syscalls (which themselves only return a boolean).
	syscallFailedFmt = "%s returned `false` for \"%s\"" +
		" - system log may have more information"
)

func Mount(point string, fsys fs.FS, options ...Option) (io.Closer, error) {
	idGen, err := newIDGenerator()
	if err != nil {
		return nil, err
	}
	// NOTE: `mountID` helps us work around the fact that
	// [cgofuse] does not currently (2023.05.30) have a way
	// to signal the caller when a system is actually ready.
	// During our mount sequence, we poll for this path
	// via the OS. If our queries succeed, the system
	// /should/ be operational at the OS level (i.e.
	// mount succeeded).
	var (
		fsID    filesystem.ID
		mountID = idGen()
		fuseSys = &fileSystem{
			mountID: posixRoot + mountID,
			FS:      fsys,
			log:     ulog.Null,
		}
		settings = settings{
			fileSystem:      fuseSys,
			readdirPlus:     ReaddirPlusCapable,
			caseInsensitive: DefaultCaseInsensitive,
		}
	)
	if err := generic.ApplyOptions(&settings, options...); err != nil {
		return nil, err
	}
	fuseHost := fuse.NewFileSystemHost(fuseSys)
	fuseHost.SetCapReaddirPlus(settings.readdirPlus)
	fuseHost.SetCapCaseInsensitive(settings.caseInsensitive)
	if err := settings.hostAdjust(fuseHost); err != nil {
		return nil, err
	}
	var args []string
	if len(settings.Options) != 0 {
		args = settings.Options
	} else {
		if id, err := filesystem.FSID(fsys); err == nil {
			fsID = id
		}
		point, args = settings.makeFuseArgs(point, fsID)
	}
	if err := doMount(fuseHost, point, mountID, args); err != nil {
		return nil, err
	}
	return generic.Closer(func() error {
		if fuseHost.Unmount() {
			return nil
		}
		return fmt.Errorf(
			syscallFailedFmt,
			"unmount", point,
		)
	}), nil
}

func doMount(fuseSys *fuse.FileSystemHost, target, mountID string, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan error)
	go safeMount(ctx, fuseSys, target, args, errs)
	statTarget := getOSTarget(target, args)
	go pollMountpoint(ctx, statTarget, mountID, errs)
	return <-errs
}

func safeMount(ctx context.Context, fuseSys *fuse.FileSystemHost, target string, args []string, errs chan<- error) {
	defer func() {
		// TODO: We should fork the lib so it errors
		// instead of panicking in this case.
		if r := recover(); r != nil {
			select {
			case errs <- disambiguateCgoPanic(r):
			case <-ctx.Done():
			}
		}
	}()
	if fuseSys.Mount(target, args) {
		return // Call succeeded.
		// (This does not mean the mountpoint is ready yet.)
	}
	select {
	case errs <- fmt.Errorf(syscallFailedFmt, "mount", target):
	case <-ctx.Done():
	}
}

func disambiguateCgoPanic(r any) error {
	if panicString, ok := r.(string); ok &&
		panicString == cgoDepPanic {
		return generic.ConstError(cgoDepMessage)
	}
	return fmt.Errorf("cgofuse panicked while attempting to mount: %v", r)
}

func pollMountpoint(ctx context.Context, target, mountID string, errs chan<- error) {
	const deadlineDuration = 16 * time.Second // Arbitrary.
	var (
		specialFile  = filepath.Join(target, mountID)
		nextInterval = makeJitterFunc(time.Microsecond)
		deadline     = time.NewTimer(deadlineDuration)
		timer        = time.NewTimer(nextInterval())
	)
	defer deadline.Stop()
	for {
		select {
		case <-deadline.C:
			timer.Stop()
			errs <- fmt.Errorf(
				"call to `Mount` did not respond in time (%v)",
				deadlineDuration,
			)
			// NOTE: this does not mean the mount did not, or
			// won't eventually succeed. We could try calling
			// `Unmount`, but we just alert the operator and
			// exit instead. They'll have more context from
			// the operating system itself than we have here.
			return
		case <-timer.C:
			// If we can access the special file,
			// then the mount succeeded.
			_, err := os.Lstat(specialFile)
			if err == nil {
				errs <- nil
				return
			}
			timer.Reset(nextInterval())
		case <-ctx.Done():
			return
		}
	}
}

func makeJitterFunc(initial time.Duration) func() time.Duration {
	// Adapted from an inlined [net/http] closure.
	const pollIntervalMax = 500 * time.Millisecond
	return func() time.Duration {
		// Add 10% jitter.
		interval := initial +
			time.Duration(rand.Intn(int(initial/10)))
		// Double and clamp for next time.
		initial *= 2
		if initial > pollIntervalMax {
			initial = pollIntervalMax
		}
		return interval
	}
}

func newIDGenerator() (func() string, error) {
	const (
		idLength       = 8
		base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	)
	return nanoid.CustomASCII(base58Alphabet, idLength)
}
