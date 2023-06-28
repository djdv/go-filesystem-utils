package cgofuse

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/u-root/uio/ulog"
	"github.com/winfsp/cgofuse/fuse"
)

type (
	closer func() error
	// Host is the cgofuse specific parameters
	// of a mount point.
	Host struct {
		Point           string   `json:"point,omitempty"`
		LogPrefix       string   `json:"logPrefix,omitempty"`
		Options         []string `json:"options,omitempty"`
		UID             uint32   `json:"uid,omitempty"`
		GID             uint32   `json:"gid,omitempty"`
		ReaddirPlus     bool     `json:"readdirPlus,omitempty"`
		DeleteAccess    bool     `json:"deleteAccess,omitempty"`
		CaseInsensitive bool     `json:"caseInsensitive,omitempty"`
		sysquirks                // Platform specific behavior.
	}
)

const (
	idOptionBody    = `id=`
	optionDelimiter = ','
	delimiterSize   = len(string(optionDelimiter))

	syscallFailedFmt = "%s returned `false` for \"%s\"" +
		" - system log may have more information"
	// [cgofuse] does not currently [2023.05.30] have a way
	// to signal the caller when a system is actually ready.
	// Our wrapper file system will respect calls to this file,
	// and the operating system may query it.
	// The name is an arbitrary base58 NanoID of length 9.
	mountedFileName = "📂FK3GQ5WBB"
	mountedFusePath = posixRoot + mountedFileName
	mountedFilePath = string(os.PathSeparator) + mountedFileName
)

func (close closer) Close() error { return close() }

func (mh *Host) HostID() filesystem.Host { return HostID }

func (mh *Host) ParseField(key, value string) error {
	const (
		pointKey           = "point"
		logPrefixKey       = "logPrefix"
		optionsKey         = "options"
		uidKey             = "uid"
		gidKey             = "gid"
		readdirPlusKey     = "readdirplus"
		deleteAccessKey    = "deleteaccess"
		caseInsensitiveKey = "caseinsensitive"
	)
	var err error
	switch key {
	case pointKey:
		mh.Point = value
	case logPrefixKey:
		mh.LogPrefix = value
	case optionsKey:
		mh.Options = mh.splitArgv(value)
	case uidKey:
		err = mh.parseID(value, &mh.UID)
	case gidKey:
		err = mh.parseID(value, &mh.GID)
	case readdirPlusKey:
		err = mh.parseBoolFlag(value, &mh.ReaddirPlus)
	case deleteAccessKey:
		err = mh.parseBoolFlag(value, &mh.DeleteAccess)
	case caseInsensitiveKey:
		err = mh.parseBoolFlag(value, &mh.CaseInsensitive)
	default:
		err = p9fs.FieldError{
			Key:   key,
			Tried: []string{pointKey},
		}
	}
	return err
}

func (mh *Host) parseID(value string, target *id) error {
	actual, err := strconv.ParseUint(value, 0, 32)
	if err != nil {
		return err
	}
	*target = id(actual)
	return nil
}

func (mh *Host) parseBoolFlag(value string, target *bool) error {
	b, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	*target = b
	return nil
}

func (mh *Host) splitArgv(argv string) (options []string) {
	var (
		tokens   = strings.Split(argv, "-")
		isDouble bool
	)
	for _, token := range tokens[1:] {
		if token == "" {
			isDouble = true
			continue
		}
		var option string
		token = strings.TrimSuffix(token, " ")
		if isDouble {
			option = "--" + token
			isDouble = false
		} else {
			option = "-" + token
		}
		options = append(options, option)
	}
	return options
}

func (mh *Host) Mount(fsys fs.FS) (io.Closer, error) {
	mh.sysquirks.mount()
	sysLog := ulog.Null
	if prefix := mh.LogPrefix; prefix != "" {
		sysLog = log.New(os.Stdout, prefix, log.Lshortfile)
	}
	var (
		fsID       filesystem.ID
		mountPoint = mh.Point
		fuseSys    = &goWrapper{
			FS:  fsys,
			log: sysLog,
		}
		fuseHost = fuse.NewFileSystemHost(fuseSys)
	)
	fuseHost.SetCapReaddirPlus(mh.ReaddirPlus)
	fuseHost.SetCapCaseInsensitive(mh.CaseInsensitive)
	fuseHost.SetCapDeleteAccess(mh.DeleteAccess)
	var (
		target string
		args   []string
	)
	if len(mh.Options) != 0 {
		target = mh.Point
		args = mh.Options
	} else {
		if idFS, ok := fsys.(filesystem.IDFS); ok {
			fsID = idFS.ID()
		}
		target, args = makeFuseArgs(fsID, mh)
	}
	if err := doMount(fuseHost, target, args); err != nil {
		return nil, err
	}
	return closer(func() error {
		if fuseHost.Unmount() {
			mh.sysquirks.unmount()
			return nil
		}
		return fmt.Errorf(
			syscallFailedFmt,
			"unmount", mountPoint,
		)
	}), nil
}

func doMount(fuseSys *fuse.FileSystemHost, target string, args []string) error {
	errs := make(chan error, 1)
	go safeMount(fuseSys, target, args, errs)
	statTarget := getOSTarget(target, args)
	go pollMountpoint(statTarget, errs)
	return <-errs
}

func safeMount(fuseSys *fuse.FileSystemHost, target string, args []string, errs chan<- error) {
	defer func() {
		// TODO: We should fork the lib so it errors
		// instead of panicking in this case.
		if r := recover(); r != nil {
			errs <- disambiguateCgoPanic(r)
		}
	}()
	if fuseSys.Mount(target, args) {
		return // Call succeeded.
	}
	errs <- fmt.Errorf(syscallFailedFmt, "mount", target)
}

func disambiguateCgoPanic(r any) error {
	if panicString, ok := r.(string); ok &&
		panicString == cgoDepPanic {
		return generic.ConstError(cgoDepMessage)
	}
	return fmt.Errorf("cgofuse panicked while attempting to mount: %v", r)
}

func idOptionPre(id uint32) (string, int) {
	var (
		idStr = strconv.Itoa(int(id))
		size  = 1 + len(idOptionBody) + len(idStr)
	)
	return idStr, size
}

func idOption(option *strings.Builder, id string, leader rune) {
	option.WriteRune(leader)
	option.WriteString(idOptionBody)
	option.WriteString(id)
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

func pollMountpoint(target string, errs chan<- error) {
	const deadlineDuration = 16 * time.Second // Arbitrary.
	var (
		specialFile  = filepath.Join(target, mountedFilePath)
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
		}
	}
}
