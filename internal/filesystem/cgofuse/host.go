//go:build !nofuse

package cgofuse

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
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
		UID             id       `json:"uid,omitempty"`
		GID             id       `json:"gid,omitempty"`
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
	if err := safeMount(fuseHost, target, args); err != nil {
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

func safeMount(fuseSys *fuse.FileSystemHost, target string, args []string) error {
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
			err := fmt.Errorf(
				syscallFailedFmt,
				"mount", target,
			)
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
