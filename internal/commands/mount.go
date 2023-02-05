package commands

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/ipfs"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
	"github.com/jaevor/go-nanoid"
	"github.com/multiformats/go-multiaddr"
)

type (
	mountSettings struct {
		helpOnly
		mountIPFSSettings
		// TODO: bind to cli params
		// TODO ID types should be raw uint;
		// used for/with numerical ID systems (Unix, FUSE, 9P, et al.).
		uid p9.UID
		gid p9.GID
	}
	mountFuseSettings struct{ helpOnly }
	mountIPFSSettings struct {
		ipfs struct {
			nodeMaddr multiaddr.Multiaddr
		}
		clientSettings
	}

	MountOption func(*mountSettings) error
)

const fuseHost filesystem.Host = "FUSE"

func supportedHosts() []filesystem.Host {
	return []filesystem.Host{
		fuseHost,
	}
}

// TODO: move; should be part of [command] pkg.
func subonlyExec[settings command.Settings[T], cmd command.ExecuteFuncArgs[settings, T], T any]() cmd {
	return func(context.Context, settings, ...string) error {
		// This command only holds subcommands
		// and has no functionality on its own.
		return command.ErrUsage
	}
}

func (set *mountSettings) BindFlags(fs *flag.FlagSet) {
	set.helpOnly.BindFlags(fs)
}

func (set *mountFuseSettings) BindFlags(fs *flag.FlagSet) {
	set.helpOnly.BindFlags(fs)
}

func (set *mountIPFSSettings) BindFlags(fs *flag.FlagSet) {
	set.clientSettings.BindFlags(fs)
	// TODO: this should be a string, not parsed client-side
	// (server may have different namespaces registered + double parse;
	// just passthrough argv[x] as-is)
	const (
		ipfsName  = "ipfs"
		ipfsUsage = "IPFS API node `maddr`"
	)
	set.ipfs.nodeMaddr = &defaultIPFSMaddr{}
	fs.Func(ipfsName, ipfsUsage, func(s string) (err error) {
		set.ipfs.nodeMaddr, err = multiaddr.NewMultiaddr(s)
		return
	})
}

// Mount constructs the command which requests
// the file system service to mount a system.
func Mount() command.Command {
	const (
		name     = "mount"
		synopsis = "Mount a file system."
		usage    = "Placeholder text."
	)
	return command.MakeCommand[*mountSettings](name, synopsis, usage,
		subonlyExec[*mountSettings](),
		command.WithSubcommands(makeMountSubcommands()...),
	)
}

func mountFuse() command.Command {
	const usage = "Placeholder text."
	var (
		formalName = string(fuseHost)
		cmdName    = strings.ToLower(formalName)
		synopsis   = fmt.Sprintf("Mount a file system via the %s API.", formalName)
	)
	return command.MakeCommand[*mountFuseSettings](cmdName, synopsis, usage,
		subonlyExec[*mountFuseSettings](),
		command.WithSubcommands(makeMountFuseSubcommands()...),
	)
}

func makeMountSubcommands() []command.Command {
	var (
		hostTable   = supportedHosts()
		subcommands = make([]command.Command, len(hostTable))
	)
	for i, hostAPI := range hostTable {
		switch hostAPI {
		case fuseHost:
			subcommands[i] = mountFuse()
		default:
			panic("unexpected API ID for host file system interface")
		}
	}
	return subcommands
}

func makeMountFuseSubcommands() []command.Command {
	const usage = "Placeholder text."
	var (
		hostName    = string(fuseHost)
		fsidTable   = p9fs.FileSystems()
		subcommands = make([]command.Command, len(fsidTable))
	)
	for i, fsid := range fsidTable {
		var (
			fsName     = string(fsid)
			subcmdName = strings.ToLower(fsName)
			synopsis   = fmt.Sprintf("Mount %s via the %s API.", fsName, hostName)
		)
		switch fsid {
		case ipfs.IPFSID, ipfs.PinFSID,
			ipfs.IPNSID, ipfs.KeyFSID:
			subcommands[i] = command.MakeCommand[*mountIPFSSettings](subcmdName, synopsis, usage,
				makeFuseIPFSExec(fuseHost, fsid),
			)
		default:
			panic("unexpected API ID for host file system interface")
		}
	}
	return subcommands
}

func makeFuseIPFSExec(host filesystem.Host, fsid filesystem.ID) func(context.Context, *mountIPFSSettings, ...string) error {
	return func(ctx context.Context, set *mountIPFSSettings, args ...string) error {
		return ipfsExecute(ctx, host, fsid, set, args...)
	}
}

func ipfsExecute(ctx context.Context, host filesystem.Host, fsid filesystem.ID,
	set *mountIPFSSettings, args ...string,
) error {
	if len(args) == 0 {
		// TODO: [command] we need to expose arguments as a concept to the library somehow.
		// Maybe an interface like
		// `ExplainArguments() []pair{name;helptext}`
		// `ParseArguments(*settings, args...) error`
		// and/or a different error value.
		// As-is, ErrUsage really only applies to niladic functions which receive arguments
		// not variadic one.
		// [f575114c-9b1d-484c-ade6-b9ce0f6887c8]
		return fmt.Errorf("%w - expected mountpoint(s)", command.ErrUsage)
	}
	const launch = true
	client, err := getClient(&set.clientSettings, launch)
	if err != nil {
		return err
	}
	ipfsMaddr := set.ipfs.nodeMaddr
	if lazy, ok := ipfsMaddr.(lazyFlag[multiaddr.Multiaddr]); ok {
		maddr, err := lazy.get()
		if err != nil {
			err = fmt.Errorf("could not retrieve IPFS node's maddr: %w"+
				"\na node's maddr can be provided explicitly via the `-ipfs=$maddr` flag",
				err)
			if cErr := client.Close(); cErr != nil {
				err = fserrors.Join(err, cErr)
			}
			return err
		}
		ipfsMaddr = maddr
	}

	mountOpts := []MountOption{
		WithIPFS(ipfsMaddr),
	}
	// TODO: assure the client always gets closed
	// otherwise we spam the daemon log.
	// TODO: client commands should take a context?
	if err := client.Mount(host, fsid, args, mountOpts...); err != nil {
		if cErr := client.Close(); cErr != nil {
			err = fserrors.Join(err, cErr)
		}
		return err
	}
	return fserrors.Join(
		client.Close(),
		ctx.Err(),
	)
}

func (c *Client) Mount(host filesystem.Host, fsid filesystem.ID, args []string, options ...MountOption) error {
	settings := mountSettings{
		uid: p9.NoUID,
		gid: p9.NoGID,
	}
	for _, setter := range options {
		if err := setter(&settings); err != nil {
			return err
		}
	}
	switch host {
	case fuseHost:
		return c.handleFuse(fsid, &settings, args)
	default:
		return errors.New("NIY")
	}
}

func (c *Client) handleFuse(fsid filesystem.ID,
	set *mountSettings, targets []string,
) (err error) {
	const (
		// Alloc hint; how many times
		// [addCloser] is called in this scope.
		// (omitting exclusive branches.)
		addCloserCount = 3
	)
	addCloser, closeWith := makeCloserFuncs(addCloserCount)
	defer func() { err = fserrors.Join(closeWith(err)...) }()

	mRoot, err := c.p9Client.Attach(p9fs.MounterName)
	if err != nil {
		return err
	}
	addCloser(mRoot)

	var (
		fuseName = string(fuseHost)
		fsidName = string(fsid)
		wname    = []string{fuseName, fsidName}
		uid      = set.uid
		gid      = set.gid
	)
	const permissions = p9fs.ReadUser | p9fs.WriteUser | p9fs.ExecuteUser |
		p9fs.ReadGroup | p9fs.ExecuteGroup |
		p9fs.ReadOther | p9fs.ExecuteOther
	idRoot, err := p9fs.MkdirAll(mRoot, wname, permissions, uid, gid)
	if err != nil {
		return err
	}
	addCloser(idRoot)
	for _, target := range targets {
		data := p9fs.IPFSMountpoint{
			ApiMaddr: set.ipfs.nodeMaddr,
			Target:   target,
		}
		bytes, err := json.Marshal(data)
		if err != nil {
			return err
		}
		name := fmt.Sprintf("%s.json", c.newID())
		if err := newMountFile(idRoot, permissions, uid, gid,
			name, bytes); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) newID() string {
	idGen := c.idGen
	if idGen == nil {
		var err error
		if idGen, err = nanoid.CustomASCII(base58Alphabet, idLength); err != nil {
			panic(err)
		}
		c.idGen = idGen
	}
	return idGen()
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
