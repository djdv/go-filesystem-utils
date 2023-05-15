package commands

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/ipfs"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
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
	mountIPFSSettings    struct{ ipfs.IPFSGuest }
	mountPinFSSettings   struct{ ipfs.PinFSGuest }
	mountIPNSSettings    struct{ ipfs.IPNSGuest }
	mountKeyFSFSSettings struct{ ipfs.KeyFSGuest }

	mountPointSettings[
		HT, GT any,
		H hostSettings[HT],
		G guestSettings[GT],
	] struct {
		Host  HT
		Guest GT
		clientSettings
	}
)

const subcommandUsage = "Must be called with a subcommand."

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

func registerCommonIPFSFlags(flagSet *flag.FlagSet,
	api *multiaddr.Multiaddr,
	timeout *time.Duration,
	nodeCache, dirCache *int,
) {
	ipfsAPIVar(flagSet, api)
	ipfsTimeoutVar(flagSet, timeout)
	ipfsNodeCacheVar(flagSet, nodeCache)
	ipfsDirCacheVar(flagSet, dirCache)
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
	*field = &defaultIPFSMaddr{}
	flagSet.Func(ipfsName, ipfsUsage, func(s string) (err error) {
		*field, err = multiaddr.NewMultiaddr(s)
		return
	})
	setDefaultValueText(flagSet, flagDefaultText{
		ipfsName: ipfsDefaultText,
	})
}

func ipfsTimeoutVar(flagSet *flag.FlagSet, field *time.Duration) {
	const (
		timeoutName  = "ipfs-timeout"
		timeoutUsage = "timeout to use when communicating" +
			" with the IPFS API" +
			"\nif <= 0, operations will remain pending" +
			" until the file or system is closed"
	)
	flagSet.DurationVar(field, timeoutName,
		1*time.Minute, timeoutUsage,
	)
}

func ipfsNodeCacheVar(flagSet *flag.FlagSet, field *int) {
	const (
		defaultCacheCount = 64
		nodeCacheName     = "ipfs-node-cache"
		nodeCacheUsage    = "number of nodes to keep in the cache" +
			"\nnegative values disable node caching"
	)
	flagSet.IntVar(field, nodeCacheName, defaultCacheCount, nodeCacheUsage)
}

func ipfsDirCacheVar(flagSet *flag.FlagSet, field *int) {
	const (
		defaultCacheCount = 64
		dirCacheName      = "ipfs-directory-cache"
		dirCacheUsage     = "number of directory entry lists to keep in the cache" +
			"\nnegative values disable directory caching"
	)
	flagSet.IntVar(field, dirCacheName, defaultCacheCount, dirCacheUsage)
}

func ipnsExpiryVar(flagSet *flag.FlagSet, field *time.Duration) {
	const (
		expiryName  = "ipns-expiry"
		expiryUsage = "`duration` of how long a node is considered" +
			"valid within the cache" +
			"\nafter this time, the node will be refreshed during" +
			" its next operation"
	)
	flagSet.DurationVar(field, expiryName, 1*time.Minute, expiryUsage)
}

func (mp *mountPointSettings[HT, GT, H, G]) BindFlags(flagSet *flag.FlagSet) {
	mp.clientSettings.BindFlags(flagSet)
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
	registerCommonIPFSFlags(flagSet,
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
	registerCommonIPFSFlags(flagSet,
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
	registerCommonIPFSFlags(flagSet,
		&set.APIMaddr,
		&set.APITimeout,
		&set.NodeCacheCount,
		&set.DirectoryCacheCount,
	)
	ipnsExpiryVar(flagSet, &set.NodeExpiry)
}

func (set *mountIPNSSettings) lazyInit() error {
	return lazyMaddr(&set.APIMaddr)
}

func (set *mountIPNSSettings) marshal(string) ([]byte, error) {
	return json.Marshal(set)
}

func (set *mountKeyFSFSSettings) BindFlags(flagSet *flag.FlagSet) {
	registerCommonIPFSFlags(flagSet,
		&set.APIMaddr,
		&set.APITimeout,
		&set.NodeCacheCount,
		&set.DirectoryCacheCount,
	)
	ipnsExpiryVar(flagSet, &set.NodeExpiry)
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
		synopsis = "Mount a file system."
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
			if err := client.Mount(host, fsid, data); err != nil {
				return fserrors.Join(err, client.Close())
			}
			if err := client.Close(); err != nil {
				return err
			}
			return ctx.Err()
		})
}

func (c *Client) Mount(host filesystem.Host, fsid filesystem.ID, data [][]byte) (err error) {
	const (
		// Alloc hint; how many times
		// [addCloser] is called in this scope.
		// (omitting exclusive branches.)
		addCloserCount = 3
	)
	addCloser, closeWith := makeCloserFuncs(addCloserCount)
	defer func() { err = fserrors.Join(closeWith(err)...) }()

	mRoot, err := (*p9.Client)(c).Attach(mountsFileName)
	if err != nil {
		return err
	}
	addCloser(mRoot)
	var (
		hostName = string(host)
		fsName   = string(fsid)
		wnames   = []string{hostName, fsName}
	)
	const ( // TODO: from options
		permissions = p9fs.ReadUser | p9fs.WriteUser | p9fs.ExecuteUser |
			p9fs.ReadGroup | p9fs.ExecuteGroup |
			p9fs.ReadOther | p9fs.ExecuteOther
		uid = p9.NoUID
		gid = p9.NoGID
	)
	idRoot, err := p9fs.MkdirAll(mRoot, wnames, permissions, uid, gid)
	if err != nil {
		return err
	}
	addCloser(idRoot)
	const (
		mountIDLength  = 9
		base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	)
	idGen, err := nanoid.CustomASCII(base58Alphabet, mountIDLength)
	if err != nil {
		return err
	}
	// TODO: unwind? (see below)
	// created []string{name1,name2}; parent.unlinkall(created)
	for _, data := range data {
		name := fmt.Sprintf("%s.json", idGen())
		if err := newMountFile(idRoot, permissions, uid, gid,
			name, data); err != nil {
			// TODO: unwind? del created mountfiles?
			return err
		}
	}
	return nil
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
		return fserrors.Join(err, idClone.Close())
	}
	if _, err := targetFile.WriteAt(data, 0); err != nil {
		return fserrors.Join(err, targetFile.Close())
	}
	if err := targetFile.FSync(); err != nil {
		if errors.Is(err, perrors.EIO) {
			// TODO: [p9 fork]
			// Our client should use a unique version string
			// that allows the server to send Rerror (string),
			// instead of Rlerror (errno).
			// Until then, we only know generally
			// "something went wrong", not what specifically.
			err = fmt.Errorf("%w: %s", err, "IPFS node may be unreachable?")
		}
		return fserrors.Join(err, targetFile.Close())
	}
	return targetFile.Close()
}

func makeCloserFuncs(size int) (func(io.Closer), func(error) []error) {
	var (
		closers   = make([]io.Closer, 0, size)
		add       = func(closer io.Closer) { closers = append(closers, closer) }
		closeWith = func(err error) (errs []error) {
			if err != nil {
				errs = append(errs, err)
			}
			for i := len(closers) - 1; i >= 0; i-- {
				if err := closers[i].Close(); err != nil {
					errs = append(errs, err)
				}
			}
			return errs
		}
	)
	return add, closeWith
}
