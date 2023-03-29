package commands

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
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
	mountSettings interface {
		getClient(autoLaunchDaemon bool) (*Client, error)
		marshalMountpoint() ([]byte, error)
	}
	mountSettingsConstraint[T any] interface {
		command.Settings[T]
		mountSettings
	}
	fuseSettings struct {
		cgofuse.MountPoint
	}
	hostCommand interface {
		command.FlagBinder
		setTarget(string) error
	}
	hostCommandConstraint[T any] interface {
		*T
		hostCommand
	}

	mountIPFSSettings  ipfs.IPFSMountPoint
	mountPinFSSettings ipfs.PinFSMountPoint
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

func makeIPFSAPIFlag(flagSet *flag.FlagSet, field *multiaddr.Multiaddr) {
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

func (set *fuseSettings) BindFlags(flagSet *flag.FlagSet) {
	// TODO the rest
	const (
		uidName = "fuse-uid"
		// TODO: real usage message
		uidUsage = "host `uid` to mount with" +
			"\nnegative values something something..."
	)
	flagSet.Func(uidName, uidUsage, func(s string) error {
		num, err := strconv.Atoi(s)
		if err != nil {
			return err
		}
		set.UID = uint32(num)
		return nil
	})
}

func (set *fuseSettings) setTarget(target string) error {
	set.Point = target
	return nil
}

func (set *mountIPFSSettings) BindFlags(flagSet *flag.FlagSet) {
	makeIPFSAPIFlag(flagSet, &set.APIMaddr)
}

func (set *mountIPFSSettings) lazy() error {
	fmt.Printf("%#T\n", set.APIMaddr)
	if lazy, ok := set.APIMaddr.(lazyFlag[multiaddr.Multiaddr]); ok {
		var err error
		if set.APIMaddr, err = lazy.get(); err != nil {
			return err
		}
	}
	return nil
}

// FIXME: we need to cascade these through embedding.
// not 1 method per type.
func (set *mountPinFSSettings) lazy() error {
	if lazy, ok := set.APIMaddr.(lazyFlag[multiaddr.Multiaddr]); ok {
		var err error
		if set.APIMaddr, err = lazy.get(); err != nil {
			return err
		}
	}
	return nil
}

func (set *mountIPFSSettings) marshalMountpoint() ([]byte, error) {
	return json.Marshal(set)
}

func (set *mountPinFSSettings) BindFlags(flagSet *flag.FlagSet) {
	makeIPFSAPIFlag(flagSet, &set.APIMaddr)
	const (
		expiryName  = "expiry"
		expiryUsage = "Pin cache `duration`" +
			"\nnegative values retain cache forever, 0 disables cache"
	)
	set.CacheExpiry = 30 * time.Second
	flagSet.Func(expiryName, expiryUsage, func(s string) (err error) {
		set.CacheExpiry, err = time.ParseDuration(s)
		return
	})
}

func (set *mountPinFSSettings) marshalMountpoint() ([]byte, error) {
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
		executeFn   = subonlyExec[*helpOnly]()
		subcommands = makeMountSubcommands()
	)
	return command.MakeCommand[*helpOnly](name, synopsis, subcommandUsage,
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
			// subCommands       = makeFSIDCommands(hostAPI)
			// subCommands       = makeFSIDCommands(hostAPI)
			// subcommandsOption = command.WithSubcommands(subCommands...)
		)
		switch hostAPI {
		case cgofuse.HostID:
			// subCommands       = makeFSIDCommands(hostAPI)
			guestCommands := makeGuestCommands[fuseSettings](hostAPI)
			subCommands[i] = command.MakeCommand[*helpOnly](
				commandName, synopsis, subcommandUsage,
				subonlyExec[*helpOnly](),
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
func subonlyExec[settings command.Settings[T], cmd command.ExecuteFuncArgs[settings, T], T any]() cmd {
	return func(context.Context, settings, ...string) error {
		// This command only holds subcommands
		// and has no functionality on its own.
		return command.ErrUsage
	}
}

// func makeGuestCommands[H hostCommand](host filesystem.Host) []command.Command {
func makeGuestCommands[
	H any,
	HC hostCommandConstraint[H],
](host filesystem.Host,
) []command.Command {
	var (
		fsidTable = supportedSystems()
		// subcommands = make([]command.Command, len(fsidTable))
		subcommands = make([]command.Command, 2)
	)
	for i, fsid := range fsidTable {
		// TODO: we should only pass in the external guest type
		// (and later the host type)
		// let makeMountCommand compose its own type internally.
		// ^ this should be compiler legal now.
		// i.e. local only anonymous type
		// type x stuct {clientSettings; G; H}
		// ^ This likely can't work because we require methods
		// that wouldn't be composable (BindFlags)
		// (at least not in 1.20's compiler)
		// ^^ Generic adapter?
		// struct [A,B] BindFlags() { self.A.BindFlags, self.B.Bindflags?}
		switch fsid {
		case ipfs.IPFSID:
			subcommands[i] = makeMountCommand[HC, mountIPFSSettings](host, fsid)
		case ipfs.PinFSID:
			subcommands[i] = makeMountCommand[HC, mountPinFSSettings](host, fsid)
		case ipfs.IPNSID, ipfs.KeyFSID,
			ipfs.MFSID:
			// TODO implement
		default:
			panic("unexpected API ID for host file system interface")
		}
	}
	return subcommands
}

func makeMountCommand[
	HC hostCommandConstraint[H],
	G, H any,
	GC guestCommandConstraint[G],
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
	return command.MakeCommand[MS](cmdName, synopsis, usage,
		// return command.MakeCommand[MS](cmdName, synopsis, usage,
		// return command.MakeCommand[*mountPointSettings[H,G]](cmdName, synopsis, usage,
		func(ctx context.Context, settings MS, args ...string) error {
			client, err := mountPreamble(settings, args...)
			if err != nil {
				return err
			}
			data := make([][]byte, len(args))
			for i, target := range args {
				// HACK: We re-use settings instead of
				// creating disparate copies.
				HC(&settings.Host).setTarget(target)
				// setMountPointTarget(settings, target)
				datum, err := settings.marshalMountpoint()
				if err != nil {
					return err
				}
				data[i] = datum
			}
			if err := client.Mount(host, fsid, data); err != nil {
				return fserrors.Join(err, client.Close())
			}
			return fserrors.Join(ctx.Err(), client.Close())
		})
}

func mountPreamble[MS mountSettingsConstraint[T], T any](settings MS, args ...string) (*Client, error) {
	if len(args) == 0 {
		err := fmt.Errorf("%w - expected mount point(s)", command.ErrUsage)
		return nil, err
	}
	const autoLaunchDaemon = true
	if err := mountInitLazyFlags(settings); err != nil {
		return nil, err
	}
	return settings.getClient(autoLaunchDaemon)
}

// HACK: An elegant way to handle this is not yet obvious.
// We either need to put interfaces on the mount point types
// at the external pkg, duplicate those definitions here, or
// write duplicate methods (`initLazy`) for each settings type.
// Or do it like this; per field check,
// which can be adapted to a list per type if needed.
func mountInitLazyFlags[MS mountSettingsConstraint[T], T any](settings MS) error {
	/*
		var apiField *multiaddr.Multiaddr
		switch typed := any(settings).(type) {
		case *mountIPFSSettings:
			apiField = &typed.APIMaddr
		case *mountPinFSSettings:
			apiField = &typed.APIMaddr
		}
		if lazy, ok := (*apiField).(lazyFlag[multiaddr.Multiaddr]); ok {
			var err error
			if *apiField, err = lazy.get(); err != nil {
				return err
			}
		}
	*/
	if lazy, ok := any(settings).(lazyInterface); ok {
		if err := lazy.lazy(); err != nil {
			return err
		}
	}
	return nil
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

	mRoot, err := (*p9.Client)(c).Attach(p9fs.MountFileName)
	if err != nil {
		return err
	}
	addCloser(mRoot)
	var (
		hostName = string(host)
		fsName   = string(fsid)
		wname    = []string{hostName, fsName}
	)
	const ( // TODO: from options
		permissions = p9fs.ReadUser | p9fs.WriteUser | p9fs.ExecuteUser |
			p9fs.ReadGroup | p9fs.ExecuteGroup |
			p9fs.ReadOther | p9fs.ExecuteOther
		uid = p9.NoUID
		gid = p9.NoGID
	)
	idRoot, err := p9fs.MkdirAll(mRoot, wname, permissions, uid, gid)
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
