//go:build !nofuse
// +build !nofuse

package cgofuse

import (
	gopath "path"
	"path/filepath"
	"strings"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
	config "github.com/ipfs/go-ipfs-config"
	"github.com/ipfs/go-ipfs/core/commands/filesystem/cgofuse/sys"
	"github.com/ipfs/go-ipfs/filesystem"
	logging "github.com/ipfs/go-log"
)

type hostBinding struct {
	nodeInterface filesystem.Interface
	log           logging.EventLogger // general operations log

	readdirplusGen      // if set, we'll bind this function with directories and call it to stat their elements
	filesWritable  bool // switch for metadata fields and operation availability

	files       fileTable // tables for open references
	directories directoryTable

	mountTimeGroup statTimeGroup // storage for various timestamps
}

func NewFuseInterface(fs filesystem.Interface) (fuselib.FileSystemInterface, error) {
	// TODO: migrate options interface from old branch
	logName := strings.ToLower(gopath.Join("fuse", fs.ID().String()))
	fuseInterface := &hostBinding{
		nodeInterface: fs,
		log:           logging.Logger(logName),
	}

	// TODO: we need to provide a function Option for swapping out methods
	// such as `Stat(name)`, `Permission(name)`, etc.
	// for now hardcoded list of special handling
	switch fs.ID() {
	case filesystem.PinFS, filesystem.IPFS:
		fuseInterface.readdirplusGen = staticStat
	case filesystem.KeyFS, filesystem.Files:
		fuseInterface.filesWritable = true
		fallthrough
	default:
		fuseInterface.readdirplusGen = dynamicStat
	}

	return fuseInterface, nil
}

func (fs *hostBinding) Init() {
	fs.log.Debugf("Init")
	defer fs.log.Debugf("Init finished")

	fs.files = newFileTable()
	fs.directories = newDirectoryTable()

	timeOfMount := fuselib.Now()

	fs.mountTimeGroup = statTimeGroup{
		atim:     timeOfMount,
		mtim:     timeOfMount,
		ctim:     timeOfMount,
		birthtim: timeOfMount,
	}
}

func (fs *hostBinding) Destroy() {
	fs.log.Debugf("Destroy")
	/* Old
	defer func() {
		if fs.destroySignal != nil {
			// TODO: close all file/dir indices, stream errors out to destroy chan
			close(fs.destroySignal)
			fs.destroySignal = nil
		}
		fs.log.Debugf("Destroy finished")
	}()
	*/
}

func (fs *hostBinding) Statfs(path string, stat *fuselib.Statfs_t) int {
	fs.log.Debugf("Statfs - HostRequest %q", path)
	return -fuselib.ENOSYS

	// FIXME: this works but needs to be done host side and somehow relayed to the client
	// otherwise the client would be getting statistics for their repo, even when remote mounting
	// (or errors if they don't have one)

	target, err := config.DataStorePath("")
	if err != nil {
		fs.log.Errorf("Statfs - Config err %q: %v", path, err)
		return -fuselib.ENOENT
	}

	errNo, err := sys.Statfs(target, stat)
	if err != nil {
		fs.log.Errorf("Statfs - err %q: %v", target, err)
	}
	return errNo
}

func (fs *hostBinding) Readlink(path string) (int, string) {
	fs.log.Debugf("Readlink - %q", path)

	switch path {
	case "/":
		fs.log.Warnf("Readlink - root path is an invalid Request")
		return -fuselib.EINVAL, ""

	case "":
		fs.log.Error("Readlink - empty Request")
		return -fuselib.ENOENT, ""
	}

	linkString, err := fs.nodeInterface.ExtractLink(path)
	if err != nil {
		fs.log.Error(err)
		return interpretError(err), ""
	}

	// NOTE: paths returned here get sent back to the FUSE library
	// they should not be native paths, regardless of their source format
	return operationSuccess, filepath.ToSlash(linkString)
}

func (fs *hostBinding) Rename(oldpath, newpath string) int {
	fs.log.Warnf("Rename - HostRequest %q->%q", oldpath, newpath)

	if err := fs.nodeInterface.Rename(oldpath, newpath); err != nil {
		fs.log.Error(err)
		return interpretError(err)
	}

	return operationSuccess
}

func (fs *hostBinding) Truncate(path string, size int64, fh uint64) int {
	fs.log.Debugf("Truncate - HostRequest {%X|%d}%q", fh, size, path)

	if size < 0 {
		return -fuselib.EINVAL
	}

	var didOpen bool
	file, err := fs.files.Get(fh) // use the handle if it's valid
	if err != nil {               // otherwise fallback to open
		file, err = fs.nodeInterface.Open(path, filesystem.IOWriteOnly)
		if err != nil {
			fs.log.Error(err)
			return interpretError(err)
		}
		didOpen = true
	}

	if err = file.Truncate(uint64(size)); err != nil {
		fs.log.Error(err)
		return interpretError(err)
	}

	if didOpen {
		if err := file.Close(); err != nil {
			fs.log.Error(err)
			return interpretError(err)
		}
	}

	return operationSuccess
}
