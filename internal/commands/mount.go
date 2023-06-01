package commands

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/ipfs"
	"github.com/djdv/p9/p9"
	"github.com/jaevor/go-nanoid"
	"github.com/multiformats/go-multiaddr"
)

type (
	hostSettings[T any] interface {
		*T
		command.FlagBinder
		marshal(arg string) ([]byte, error)
	}
	fuseSettings struct {
		cgofuse.Host
		haveFUSEFlags bool
	}

	guestSettings[T any] interface {
		*T
		command.FlagBinder
		marshal(arg string) ([]byte, error)
	}
	mountSettings struct {
		permissions p9.FileMode
		uid         p9.UID
		gid         p9.GID
	}
	mountIPFSSettings struct {
		ipfs.IPFSGuest
		mountSettings
	}
	mountPinFSSettings struct {
		ipfs.PinFSGuest
		mountSettings
	}
	mountIPNSSettings struct {
		ipfs.IPNSGuest
		mountSettings
	}
	mountKeyFSFSSettings struct {
		ipfs.KeyFSGuest
		mountSettings
	}
	mountCmdSettings struct {
		clientSettings
		options []MountOption
	}
	mountPointSettings[
		HT, GT any,
		H hostSettings[HT],
		G guestSettings[GT],
	] struct {
		Host  HT
		Guest GT
		mountCmdSettings
	}

	MountOption func(*mountSettings) error
)

const (
	subcommandUsage            = "Must be called with a subcommand."
	mountAPIPermissionsDefault = p9fs.ReadUser | p9fs.WriteUser | p9fs.ExecuteUser |
		p9fs.ReadGroup | p9fs.ExecuteGroup |
		p9fs.ReadOther | p9fs.ExecuteOther
	mountAPIUIDDefault = p9.NoUID
	mountAPIGIDDefault = p9.NoGID
)

func WithPermissions(permissions p9.FileMode) MountOption {
	return func(ms *mountSettings) error {
		ms.permissions = permissions
		return nil
	}
}

func WithUID(uid p9.UID) MountOption {
	return func(ms *mountSettings) error {
		ms.uid = uid
		return nil
	}
}

func WithGID(gid p9.GID) MountOption {
	return func(ms *mountSettings) error {
		ms.gid = gid
		return nil
	}
}

func supportedHosts() []filesystem.Host {
	return []filesystem.Host{
		cgofuse.HostID,
	}
}

func supportedSystems() []filesystem.ID {
	return []filesystem.ID{
		ipfs.IPFSID,
		ipfs.PinFSID,
		ipfs.IPNSID,
		ipfs.KeyFSID,
	}
}

func (set *mountCmdSettings) BindFlags(flagSet *flag.FlagSet) {
	set.clientSettings.BindFlags(flagSet)
	const (
		prefix   = "api-"
		uidName  = prefix + "uid"
		uidUsage = "file owner's `uid`"
	)
	uidDefaultText := idString(mountAPIUIDDefault)
	flagSet.Func(uidName, uidUsage, func(s string) error {
		uid, err := parseID[p9.UID](s)
		if err != nil {
			return err
		}
		set.options = append(set.options, WithUID(uid))
		return nil
	})
	const (
		gidName  = prefix + "gid"
		gidUsage = "file owner's `gid`"
	)
	gidDefaultText := idString(mountAPIGIDDefault)
	flagSet.Func(gidName, gidUsage, func(s string) error {
		gid, err := parseID[p9.GID](s)
		if err != nil {
			return err
		}
		set.options = append(set.options, WithGID(gid))
		return nil
	})
	const (
		permissionsName  = prefix + "permissions"
		permissionsUsage = "`permissions` to use when creating service files"
	)
	var (
		permissions            = fs.FileMode(mountAPIPermissionsDefault &^ p9.FileModeMask)
		permissionsDefaultText = modeToSymbolicPermissions(permissions)
	)
	flagSet.Func(permissionsName, permissionsUsage, func(s string) error {
		parsedPermissions, err := parsePOSIXPermissions(permissions, s)
		if err != nil {
			return err
		}
		permissions = parsedPermissions
		// TODO: [2023.05.20]
		// patch `.Permissions()` method in 9P library.
		// For whatever reason the (unexported)
		// const `p9.permissionsMask` is defined as `01777`
		// but should be `0o7777`
		permissions9 := modeFromFS(permissions) &^ p9.FileModeMask
		set.options = append(set.options, WithPermissions(permissions9))
		return nil
	})
	setDefaultValueText(flagSet, flagDefaultText{
		uidName:         uidDefaultText,
		gidName:         gidDefaultText,
		permissionsName: permissionsDefaultText,
	})
}

func registerCommonIPFSFlags(guest filesystem.ID, flagSet *flag.FlagSet,
	api *multiaddr.Multiaddr,
	timeout *time.Duration,
	nodeCache, dirCache *int,
) {
	flagPrefix := strings.ToLower(string(guest)) + "-"
	ipfsAPIVar(flagSet, api)
	ipfsTimeoutVar(flagPrefix, flagSet, timeout)
	ipfsNodeCacheVar(flagPrefix, flagSet, nodeCache)
	ipfsDirCacheVar(flagPrefix, flagSet, dirCache)
}

func ipfsAPIVar(flagSet *flag.FlagSet, field *multiaddr.Multiaddr) {
	const (
		ipfsName  = "ipfs"
		ipfsUsage = "IPFS API node `maddr`"
	)
	ipfsDefaultText := fmt.Sprintf("parses: %s, %s",
		filepath.Join("$"+ipfsConfigEnv, ipfsAPIFileName),
		filepath.Join(ipfsConfigDefaultDir, ipfsAPIFileName),
	)
	*field = &defaultIPFSMaddr{flagName: ipfsName}
	flagSet.Func(ipfsName, ipfsUsage, func(s string) (err error) {
		*field, err = multiaddr.NewMultiaddr(s)
		return
	})
	setDefaultValueText(flagSet, flagDefaultText{
		ipfsName: ipfsDefaultText,
	})
}

func ipfsTimeoutVar(namePrefix string, flagSet *flag.FlagSet, field *time.Duration) {
	timeoutName := namePrefix + "timeout"
	const timeoutUsage = "timeout to use when communicating" +
		" with the IPFS API" +
		"\nif <= 0, operations will remain pending" +
		" until the file or system is closed"
	flagSet.DurationVar(field, timeoutName,
		1*time.Minute, timeoutUsage,
	)
}

func ipfsNodeCacheVar(namePrefix string, flagSet *flag.FlagSet, field *int) {
	nodeCacheName := namePrefix + "node-cache"
	const (
		defaultCacheCount = 64
		nodeCacheUsage    = "number of nodes to keep in the cache" +
			"\nnegative values disable node caching"
	)
	flagSet.IntVar(field, nodeCacheName, defaultCacheCount, nodeCacheUsage)
}

func ipfsDirCacheVar(namePrefix string, flagSet *flag.FlagSet, field *int) {
	dirCacheName := namePrefix + "directory-cache"
	const (
		defaultCacheCount = 64
		dirCacheUsage     = "number of directory entry lists to keep in the cache" +
			"\nnegative values disable directory caching"
	)
	flagSet.IntVar(field, dirCacheName, defaultCacheCount, dirCacheUsage)
}

func ipnsExpiryVar(namePrefix string, flagSet *flag.FlagSet, field *time.Duration) {
	expiryName := namePrefix + "expiry"
	const (
		expiryUsage = "`duration` of how long a node is considered" +
			"valid within the cache" +
			"\nafter this time, the node will be refreshed during" +
			" its next operation"
	)
	flagSet.DurationVar(field, expiryName, 1*time.Minute, expiryUsage)
}

func (mp *mountPointSettings[HT, GT, H, G]) BindFlags(flagSet *flag.FlagSet) {
	mp.mountCmdSettings.BindFlags(flagSet)
	H(&mp.Host).BindFlags(flagSet)
	G(&mp.Guest).BindFlags(flagSet)
}

func (mp *mountPointSettings[HT, GT, H, G]) lazyInit() error {
	if host, ok := any(&mp.Host).(lazyInitializer); ok {
		if err := host.lazyInit(); err != nil {
			return err
		}
	}
	if guest, ok := any(&mp.Guest).(lazyInitializer); ok {
		if err := guest.lazyInit(); err != nil {
			return err
		}
	}
	return nil
}

func (mp *mountPointSettings[HT, GT, H, G]) marshalMountpoints(args ...string) ([][]byte, error) {
	if len(args) == 0 {
		args = []string{""}
	}
	data := make([][]byte, len(args))
	for i, arg := range args {
		hostData, err := H(&mp.Host).marshal(arg)
		if err != nil {
			return nil, err
		}
		guestData, err := G(&mp.Guest).marshal(arg)
		if err != nil {
			return nil, err
		}
		datum, err := json.Marshal(struct {
			Host  json.RawMessage `json:"host,omitempty"`
			Guest json.RawMessage `json:"guest,omitempty"`
		}{
			Host:  hostData,
			Guest: guestData,
		})
		if err != nil {
			return nil, err
		}
		data[i] = datum
	}
	return data, nil
}

func (set *fuseSettings) BindFlags(flagSet *flag.FlagSet) {
	const (
		host    = "fuse"
		prefix  = host + "-"
		uidName = prefix + "uid"
		gidName = prefix + "gid"
	)
	flagMonitor := &set.haveFUSEFlags
	bindFUSEUIDFlag(uidName, &set.UID, flagSet, flagMonitor)
	bindFUSEGIDFlag(gidName, &set.GID, flagSet, flagMonitor)
	const (
		optionsName  = prefix + "options"
		optionsUsage = "raw options passed directly to mount" +
			"\nmust be specified once per `FUSE flag`" +
			"\n (E.g. `-" + optionsName +
			" \"-o uid=0,gid=0\" -" +
			optionsName + "\"--VolumePrefix=somePrefix\"`)"
	)
	flagSet.Func(optionsName, optionsUsage, func(s string) error {
		set.Options = append(set.Options, s)
		return nil
	})
	const (
		logName  = prefix + "log"
		logUsage = "sets a log `prefix` and enables logging in FUSE operations"
	)
	flagSet.StringVar(&set.LogPrefix, logName, "", logUsage)
	const (
		readdirName  = prefix + "readdir-plus"
		readdirUsage = "informs the host that the hosted file system has the readdir-plus capability"
	)
	readdirPlusCapible := runtime.GOOS == "windows"
	flagSet.BoolVar(&set.ReaddirPlus, readdirName, readdirPlusCapible, readdirUsage)
	const (
		caseName  = prefix + "case-insensitive"
		caseUsage = "informs the host that the hosted file system is case insensitive"
	)
	flagSet.BoolVar(&set.CaseInsensitive, caseName, false, caseUsage)
	const (
		deleteName  = prefix + "delete-access"
		deleteUsage = "informs the host that the hosted file system implements \"Access\" which understands the \"DELETE_OK\" flag"
	)
	flagSet.BoolVar(&set.CaseInsensitive, deleteName, false, deleteUsage)
}

func bindFUSEUIDFlag(name string, reference *uint32,
	flagSet *flag.FlagSet, setMonitor *bool,
) {
	bindFUSEIDFlag(name, "uid", reference, flagSet, setMonitor)
}

func bindFUSEGIDFlag(name string, reference *uint32,
	flagSet *flag.FlagSet, setMonitor *bool,
) {
	bindFUSEIDFlag(name, "gid", reference, flagSet, setMonitor)
}

func (set *fuseSettings) marshal(arg string) ([]byte, error) {
	if arg == "" &&
		set.Options == nil {
		err := fmt.Errorf(
			"%w - expected mount point",
			command.ErrUsage,
		)
		return nil, err
	}
	if set.Options != nil &&
		set.haveFUSEFlags {
		err := fmt.Errorf("%w - cannot combine"+
			" built-in/common FUSE flags"+
			" with raw/platform-specific flags"+
			" (the former can be "+
			"provided manually within the latter)",
			command.ErrUsage,
		)
		return nil, err
	}
	host := set.Host
	host.Point = arg
	return json.Marshal(host)
}

func lazyMaddr(maddrPtr *multiaddr.Multiaddr) error {
	if lazy, ok := (*maddrPtr).(lazyFlag[multiaddr.Multiaddr]); ok {
		maddr, err := lazy.get()
		if err != nil {
			return err
		}
		*maddrPtr = maddr
	}
	return nil
}

func (set *mountIPFSSettings) BindFlags(flagSet *flag.FlagSet) {
	registerCommonIPFSFlags(ipfs.IPFSID, flagSet,
		&set.APIMaddr,
		&set.APITimeout,
		&set.NodeCacheCount,
		&set.DirectoryCacheCount,
	)
}

func (set *mountIPFSSettings) lazyInit() error {
	return lazyMaddr(&set.APIMaddr)
}

func (set *mountIPFSSettings) marshal(string) ([]byte, error) {
	return json.Marshal(set)
}

func (set *mountPinFSSettings) BindFlags(flagSet *flag.FlagSet) {
	registerCommonIPFSFlags(ipfs.PinFSID, flagSet,
		&set.APIMaddr,
		&set.APITimeout,
		&set.NodeCacheCount,
		&set.DirectoryCacheCount,
	)
	const (
		expiryName  = "pinfs-expiry"
		expiryUsage = "`duration` pins are cached for" +
			"\nnegative values retain cache forever, 0 disables cache"
		expiryDefault = 30 * time.Second
	)
	flagSet.DurationVar(&set.CacheExpiry, expiryName, expiryDefault, expiryUsage)
}

func (set *mountPinFSSettings) lazyInit() error {
	return lazyMaddr(&set.APIMaddr)
}

func (set *mountPinFSSettings) marshal(string) ([]byte, error) {
	return json.Marshal(set)
}

func (set *mountIPNSSettings) BindFlags(flagSet *flag.FlagSet) {
	registerCommonIPFSFlags(ipfs.IPNSID, flagSet,
		&set.APIMaddr,
		&set.APITimeout,
		&set.NodeCacheCount,
		&set.DirectoryCacheCount,
	)
	flagPrefix := strings.ToLower(string(ipfs.IPNSID)) + "-"
	ipnsExpiryVar(flagPrefix, flagSet, &set.NodeExpiry)
}

func (set *mountIPNSSettings) lazyInit() error {
	return lazyMaddr(&set.APIMaddr)
}

func (set *mountIPNSSettings) marshal(string) ([]byte, error) {
	return json.Marshal(set)
}

func (set *mountKeyFSFSSettings) BindFlags(flagSet *flag.FlagSet) {
	registerCommonIPFSFlags(ipfs.KeyFSID, flagSet,
		&set.APIMaddr,
		&set.APITimeout,
		&set.NodeCacheCount,
		&set.DirectoryCacheCount,
	)
	flagPrefix := strings.ToLower(string(ipfs.KeyFSID)) + "-"
	ipnsExpiryVar(flagPrefix, flagSet, &set.NodeExpiry)
}

func (set *mountKeyFSFSSettings) lazyInit() error {
	return lazyMaddr(&set.APIMaddr)
}

func (set *mountKeyFSFSSettings) marshal(string) ([]byte, error) {
	return json.Marshal(set)
}

// Mount constructs the command which requests
// the file system service to mount a system.
func Mount() command.Command {
	const (
		name     = "mount"
		synopsis = "Mount file systems."
	)
	var (
		executeFn   = subonlyExec[*command.HelpArg]()
		subcommands = makeMountSubcommands()
	)
	return command.MustMakeCommand[*command.HelpArg](name, synopsis, subcommandUsage,
		executeFn,
		command.WithSubcommands(subcommands...),
	)
}

func makeMountSubcommands() []command.Command {
	var (
		hostTable   = supportedHosts()
		subCommands = make([]command.Command, len(hostTable))
	)
	for i, hostAPI := range hostTable {
		var (
			formalName  = string(hostAPI)
			commandName = strings.ToLower(formalName)
			synopsis    = fmt.Sprintf("Mount a file system via the %s API.", formalName)
		)
		switch hostAPI {
		case cgofuse.HostID:
			guestCommands := makeGuestCommands[fuseSettings](hostAPI)
			subCommands[i] = command.MustMakeCommand[*command.HelpArg](
				commandName, synopsis, subcommandUsage,
				subonlyExec[*command.HelpArg](),
				command.WithSubcommands(guestCommands...),
			)
		default:
			err := fmt.Errorf("unexpected Host API: %v", hostAPI)
			panic(err)
		}
	}
	return subCommands
}

// TODO: move; should be part of [command] pkg.
func subonlyExec[settings command.Settings[T], cmd command.ExecuteFunc[settings, T], T any]() cmd {
	return func(_ context.Context, _ settings) error {
		// This command only holds subcommands
		// and has no functionality on its own.
		return command.ErrUsage
	}
}

// func makeGuestCommands[H hostCommand](host filesystem.Host) []command.Command {
func makeGuestCommands[
	H any,
	HC hostSettings[H],
](host filesystem.Host,
) []command.Command {
	var (
		fsidTable   = supportedSystems()
		subcommands = make([]command.Command, len(fsidTable))
	)
	for i, fsid := range fsidTable {
		switch fsid {
		case ipfs.IPFSID:
			subcommands[i] = makeMountCommand[HC, mountIPFSSettings](host, fsid)
		case ipfs.PinFSID:
			subcommands[i] = makeMountCommand[HC, mountPinFSSettings](host, fsid)
		case ipfs.IPNSID:
			subcommands[i] = makeMountCommand[HC, mountIPNSSettings](host, fsid)
		case ipfs.KeyFSID:
			subcommands[i] = makeMountCommand[HC, mountKeyFSFSSettings](host, fsid)
		default:
			panic("unexpected API ID for host file system interface")
		}
	}
	return subcommands
}

func makeMountCommand[
	HC hostSettings[H],
	G, H any,
	GC guestSettings[G],
](host filesystem.Host, fsid filesystem.ID,
) command.Command {
	const usage = "Placeholder text."
	var (
		hostFormalName  = string(host)
		guestFormalName = string(fsid)
		cmdName         = strings.ToLower(guestFormalName)
		synopsis        = fmt.Sprintf("Mount %s via the %s API.", guestFormalName, hostFormalName)
	)
	type MS = *mountPointSettings[H, G, HC, GC]
	return command.MustMakeCommand[MS](cmdName, synopsis, usage,
		func(ctx context.Context, settings MS, args ...string) error {
			if err := settings.lazyInit(); err != nil {
				return err
			}
			data, err := settings.marshalMountpoints(args...)
			if err != nil {
				return err
			}
			const autoLaunchDaemon = true
			client, err := settings.getClient(autoLaunchDaemon)
			if err != nil {
				return err
			}
			options := settings.mountCmdSettings.options
			if err := client.Mount(host, fsid, data, options...); err != nil {
				return errors.Join(err, client.Close())
			}
			if err := client.Close(); err != nil {
				return err
			}
			return ctx.Err()
		})
}

func (c *Client) Mount(host filesystem.Host, fsid filesystem.ID, data [][]byte, options ...MountOption) error {
	set := mountSettings{
		permissions: mountAPIPermissionsDefault,
		uid:         mountAPIUIDDefault,
		gid:         mountAPIGIDDefault,
	}
	for _, setter := range options {
		if err := setter(&set); err != nil {
			return err
		}
	}
	mounts, err := (*p9.Client)(c).Attach(mountsFileName)
	if err != nil {
		return err
	}
	var (
		hostName    = string(host)
		fsName      = string(fsid)
		wnames      = []string{hostName, fsName}
		permissions = set.permissions
		uid         = set.uid
		gid         = set.gid
	)
	guests, err := p9fs.MkdirAll(mounts, wnames, permissions, uid, gid)
	if err != nil {
		err = receiveError(mounts, err)
		return errors.Join(err, mounts.Close())
	}
	const (
		mountIDLength  = 9
		base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	)
	idGen, err := nanoid.CustomASCII(base58Alphabet, mountIDLength)
	if err != nil {
		return errors.Join(err, mounts.Close(), guests.Close())
	}
	var (
		errs            []error
		filePermissions = permissions ^ (p9fs.ExecuteOther | p9fs.ExecuteGroup | p9fs.ExecuteUser)
	)
	for _, data := range data {
		name := fmt.Sprintf("%s.json", idGen())
		if err := newMountFile(guests, filePermissions, uid, gid,
			name, data); err != nil {
			errs = append(errs, err)
		}
	}
	if errs != nil {
		err = errors.Join(errs...)
	}
	err = errors.Join(err, guests.Close())
	if err != nil {
		err = receiveError(mounts, err)
	}
	return errors.Join(err, mounts.Close())
}

func newMountFile(idRoot p9.File,
	permissions p9.FileMode, uid p9.UID, gid p9.GID,
	name string, data []byte,
) error {
	_, idClone, err := idRoot.Walk(nil)
	if err != nil {
		return err
	}
	targetFile, _, _, err := idClone.Create(name, p9.WriteOnly, permissions, uid, gid)
	if err != nil {
		return errors.Join(err, idClone.Close())
	}
	// NOTE: targetFile and idClone are now aliased
	// (same fid because of `Create`).
	if _, err := targetFile.WriteAt(data, 0); err != nil {
		return errors.Join(err, targetFile.Close())
	}
	return targetFile.Close()
}
