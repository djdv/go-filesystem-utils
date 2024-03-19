package cgofuse

import (
	"fmt"
	"os"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/u-root/uio/ulog"
)

type (
	settings struct {
		*fileSystem
		lastOptionName string
		Options        []string
		uid            uint32
		gid            uint32
		uidValid,
		gidValid,
		readdirPlus,
		caseInsensitive bool
	}
	Option func(*settings) error
)

// Default values used by [New].
// (Platform specific.)
const (
	ReaddirPlusCapable     = readdirPlusCapable
	DefaultCaseInsensitive = false
)

const rawOptionsName = "WithRawOptions"

func (settings *settings) fillDefaults() error {
	// NOTE: On Windows, `os.Get{u,g}id` returns `-1`.
	// WinFSP automatically maps that ID to the
	// current user's ID. An alternative is to use
	// `fsptool.exe id "$Username"` (or a Go port of it)
	// to get the values ourselves; but isn't necessary.
	if !settings.uidValid {
		settings.uid = uint32(os.Getuid())
	}
	if !settings.gidValid {
		settings.gid = uint32(os.Getgid())
	}
	return nil
}

func (set *settings) checkConflict() error {
	last := set.lastOptionName
	if set.Options != nil && last != "" {
		return fmt.Errorf(
			"cannot combine "+
				rawOptionsName+
				" with built-in option %s",
			last,
		)
	}
	return nil
}

// Appends raw string options to be passed directly
// to the FUSE implementation's `fuse_new` call.
// E.g. `WithRawOptions("-o uid=0,gid=0", "--VolumePrefix=somePrefix")`
// This option will return an error if combined with other options
// that modify the `fuse_new` parameters, such as [WithUID], [WithGID], etc.
func WithRawOptions(options ...string) Option {
	return func(settings *settings) error {
		if settings.Options != nil {
			return generic.OptionAlreadySet(rawOptionsName)
		}
		settings.Options = options
		return settings.checkConflict()
	}
}

// Supplies the mountpoint owner's UID.
func WithUID(uid uint32) Option {
	const name = "WithUID"
	return func(settings *settings) error {
		if settings.uidValid {
			return generic.OptionAlreadySet(name)
		}
		settings.uid = uid
		settings.uidValid = true
		settings.lastOptionName = name
		return settings.checkConflict()
	}
}

// Supplies the mountpoint owner's GID.
func WithGID(gid uint32) Option {
	const name = "WithGID"
	return func(settings *settings) error {
		if settings.uidValid {
			return generic.OptionAlreadySet(name)
		}
		settings.gid = gid
		settings.gidValid = true
		settings.lastOptionName = name
		return settings.checkConflict()
	}
}

// Provides a logger for the system to use.
func WithLog(log ulog.Logger) Option {
	const name = "WithLog"
	return func(settings *settings) error {
		err := generic.ErrIfOptionWasSet(
			name, settings.log, ulog.Null,
		)
		settings.log = log
		return err
	}
}

// CanReaddirPlus informs the mounter whether the host's FUSE
// implementation supports filling in a file's stat information
// during a `readdir` operation.
func CanReaddirPlus(b bool) Option {
	return func(settings *settings) error {
		settings.readdirPlus = b
		return nil
	}
}

// IsCaseInsensitive informs the FUSE library
// whether the file system being mounted is case-sensitive.
func IsCaseInsensitive(b bool) Option {
	return func(settings *settings) error {
		settings.caseInsensitive = b
		return nil
	}
}

// DenyDelete provides a list of paths
// that will be checked when the `DELETE_OK` flag
// is passed to `access`.
// Paths must be in POSIX format, exactly as they would appear
// as parameters received by FUSE operations.
// E.g. `/nounlink` would be considered in `access(/nounlink, DELETE_OK)`.
// This is a WinFSP specific option and flag;
// providing it on other platforms will result in an error.
func DenyDelete(paths ...string) Option {
	const name = "DenyDelete"
	// Supplementary note:
	// On POSIX systems, directories with
	// "write" permissions can delete their entires.
	// Windows has a "delete" ACL for both directories
	// and files, allowing individual files to deny
	// delete access even if their parent has 'delete' permissions.
	return func(settings *settings) error {
		if settings.fileSystem.deleteAccess != nil {
			return generic.OptionAlreadySet(name)
		}
		settings.fileSystem.deleteAccess = paths
		return nil
	}
}
