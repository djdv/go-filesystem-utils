package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	golog "log"
	"net"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/ipfs"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	p9net "github.com/djdv/go-filesystem-utils/internal/net/9p"
	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/u-root/uio/ulog"
)

type (
	daemonSettings struct {
		serverMaddr  multiaddr.Multiaddr
		exitInterval time.Duration
		nineIDs
		commonSettings
		permissions fs.FileMode
	}
	ninePath   = *atomic.Uint64
	fileSystem struct {
		root interface {
			p9.File
			p9.Attacher
		}
		mount   mountSubsystem
		listen  listenSubsystem
		control controlSubsystem
	}
	mountSubsystem struct {
		*p9fs.MountFile
		name string
	}
	listenSubsystem struct {
		*p9fs.Listener
		listeners <-chan manet.Listener
		name      string
	}
	controlSubsystem struct {
		*p9fs.Directory
		name string
		shutdown
	}
	shutdown struct {
		*p9fs.ChannelFile
		ch   <-chan []byte
		name string
	}
	daemonSystem struct {
		log ulog.Logger
		fileSystem
	}
	handleFunc = func(io.ReadCloser, io.WriteCloser) error
	serveFunc  = func(manet.Listener) error
	checkFunc  = func() (bool, shutdownDisposition, error)

	stopperChan  = chan shutdownDisposition
	stopperRead  = <-chan shutdownDisposition
	stopperWrite = chan<- shutdownDisposition

	mountHost[T any] interface {
		*T
		p9fs.FieldParser
		p9fs.Mounter
	}
	mountGuest[T any] interface {
		*T
		p9fs.FieldParser
		p9fs.SystemMaker
	}
	mountPoint[
		HT, GT any,
		H mountHost[HT],
		G mountGuest[GT],
	] struct {
		Host  HT
		Guest GT
	}
)

const (
	errServe               = generic.ConstError("encountered error while serving")
	errShutdownDisposition = generic.ConstError("invalid shutdown disposition")

	// TODO: [Ame] docs.
	// serverRootName defines a name which servers and clients may use
	// to refer to the service in namespace oriented APIs.
	serverRootName = "fs"

	// TODO: [Ame] docs.
	// serverName defines a name which servers and clients may use
	// to form or find connections to a named server instance.
	// (E.g. a Unix socket of path `.../$ServerRootName/$serverName`.)
	serverName = "server"

	serverFlagName    = "server"
	exitAfterFlagName = "exit-after"
)

func (mp *mountPoint[HT, GT, H, G]) ParseField(key, value string) error {
	const (
		hostPrefix  = "host."
		guestPrefix = "guest."
	)
	var (
		prefix  string
		parseFn func(_, _ string) error
	)
	switch {
	case strings.HasPrefix(key, hostPrefix):
		prefix = hostPrefix
		parseFn = H(&mp.Host).ParseField
	case strings.HasPrefix(key, guestPrefix):
		prefix = guestPrefix
		parseFn = G(&mp.Guest).ParseField
	default:
		const wildcard = "*"
		return p9fs.FieldError{
			Key:   key,
			Tried: []string{hostPrefix + wildcard, guestPrefix + wildcard},
		}
	}
	baseKey := key[len(prefix):]
	err := parseFn(baseKey, value)
	if err == nil {
		return nil
	}
	var fErr p9fs.FieldError
	if !errors.As(err, &fErr) {
		return err
	}
	tried := fErr.Tried
	for i, e := range fErr.Tried {
		tried[i] = prefix + e
	}
	fErr.Tried = tried
	return fErr
}

func (mp *mountPoint[HT, GT, H, G]) MakeFS() (fs.FS, error) {
	return G(&mp.Guest).MakeFS()
}

func (mp *mountPoint[HT, GT, H, G]) Mount(fsys fs.FS) (io.Closer, error) {
	return H(&mp.Host).Mount(fsys)
}

func (set *daemonSettings) BindFlags(flagSet *flag.FlagSet) {
	set.commonSettings.BindFlags(flagSet)
	const (
		sockName  = serverFlagName
		sockUsage = "listening socket `maddr`"
	)
	var sockDefaultText string
	{
		maddrs, err := userServiceMaddrs()
		if err != nil {
			panic(err)
		}
		sockDefault := maddrs[0]
		sockDefaultText = sockDefault.String()
		set.serverMaddr = sockDefault
	}
	flagSet.Func(sockName, sockUsage, func(s string) (err error) {
		set.serverMaddr, err = multiaddr.NewMultiaddr(s)
		return
	})
	const (
		exitFlag  = exitAfterFlagName
		exitUsage = "check every `interval` (e.g. \"30s\") and shutdown the daemon if its idle"
	)
	flagSet.DurationVar(&set.exitInterval, exitFlag, 0, exitUsage)
	const (
		uidName        = "uid"
		uidDefaultText = "nobody"
		uidUsage       = "file owner's `uid`"
	)
	set.uid = p9.NoUID
	flagSet.Func(uidName, uidUsage, func(s string) (err error) {
		set.uid, err = parseID[p9.UID](s)
		return
	})
	const (
		gidName        = "gid"
		gidDefaultText = "nobody"
		gidUsage       = "file owner's `gid`"
	)
	set.gid = p9.NoGID
	flagSet.Func(gidName, gidUsage, func(s string) (err error) {
		set.gid, err = parseID[p9.GID](s)
		return
	})
	const (
		permissionsName        = "api-permissions"
		permissionsUsage       = "`permissions` to use when creating service files"
		permissionsDefault     = 0o751            // Skip parsing and direct assign.
		permissionsDefaultText = "u=rwx,g=rx,o=x" // Make sure these values stay in sync.
	)
	set.permissions = permissionsDefault
	flagSet.Func(permissionsName, permissionsUsage, func(s string) (err error) {
		set.permissions, err = parsePOSIXPermissions(s)
		return
	})
	setDefaultValueText(flagSet, flagDefaultText{
		uidName:         uidDefaultText,
		gidName:         gidDefaultText,
		sockName:        sockDefaultText,
		permissionsName: permissionsDefaultText,
	})
}

// Daemon constructs the command which
// hosts the file system service server.
func Daemon() command.Command {
	const (
		name     = "daemon"
		synopsis = "Hosts the service."
		usage    = "Placeholder text."
	)
	return command.MustMakeCommand[*daemonSettings](name, synopsis, usage, daemonExecute)
}

func daemonExecute(ctx context.Context, set *daemonSettings) error {
	dCtx, cancel := context.WithCancel(ctx)
	system, err := newSystem(dCtx, set)
	defer cancel()
	if err != nil {
		return err
	}
	var (
		fsys              = system.fileSystem
		log               = system.log
		listenSys         = fsys.listen
		server, serveErrs = makeAndStartServer(fsys, log)
		stopper, stopErrs = makeStopper(dCtx, cancel, fsys.mount, server, log)
		shutdownCh        = system.control.shutdown.ch
		shutdownErrs      = watchShutdown(dCtx, shutdownCh, stopper, log)
		serverMaddr       = set.serverMaddr
		listenerFS        = listenSys.Listener
		errs              = []<-chan error{
			serveErrs, stopErrs, shutdownErrs,
		}
		aggregateErrs = func() (err error) {
			for _, e := range aggregate(errs...) {
				err = fserrors.Join(err, e)
			}
			if cErr := ctx.Err(); cErr != nil {
				cErr = fmt.Errorf("daemon: %w", cErr)
				err = fserrors.Join(cErr, err)
			}
			return err
		}
	)
	relayOSSignal(dCtx, os.Interrupt, stopper)
	watchCtx(ctx, stopper)

	permissions9 := p9.ModeFromOS(set.permissions)
	if err := p9fs.Listen(listenerFS, serverMaddr, permissions9); err != nil {
		stopper <- immediateShutdown
		const maddrFmt = "net: could not listen on: %s - %w"
		err = fmt.Errorf(maddrFmt, serverMaddr, err)
		return fserrors.Join(err, aggregateErrs())
	}
	if isPipe(os.Stdin) {
		errs = append(errs, handleStdio(server.Handle))
	}
	if idleCheck := set.exitInterval; idleCheck != 0 {
		errs = append(errs, stopWhen(dCtx, idleCheck, stopper,
			makeMountChecker(system.mount, log)))
	}

	const emptyInterval = time.Hour
	errs = append(errs, stopWhen(dCtx, emptyInterval, stopper,
		makeEmptyChecker(system, log)))
	return aggregateErrs()
}

func newSystem(ctx context.Context, set *daemonSettings) (*daemonSystem, error) {
	var (
		uid       = set.uid
		gid       = set.gid
		fsys, err = newFileSystem(ctx, uid, gid)
		system    = &daemonSystem{
			fileSystem: fsys,
			log:        newDaemonLog(set.verbose),
		}
	)
	return system, err
}

func newDaemonLog(verbose bool) ulog.Logger {
	if !verbose {
		return ulog.Null
	}
	const (
		prefix = "⬆️ server - "
		flags  = golog.Lshortfile
	)
	return golog.New(os.Stderr, prefix, flags)
}

func commonOptions[OT p9fs.Options](parent p9.File, child string,
	path ninePath, uid p9.UID, gid p9.GID, permissions p9.FileMode,
) []OT {
	return []OT{
		p9fs.WithParent[OT](parent, child),
		p9fs.WithPath[OT](path),
		p9fs.WithUID[OT](uid),
		p9fs.WithGID[OT](gid),
		p9fs.WithPermissions[OT](permissions),
	}
}

func newFileSystem(ctx context.Context, uid p9.UID, gid p9.GID) (fileSystem, error) {
	const permissions = p9fs.ReadUser | p9fs.WriteUser | p9fs.ExecuteUser |
		p9fs.ReadGroup | p9fs.ExecuteGroup |
		p9fs.ReadOther | p9fs.ExecuteOther
	var (
		path    = new(atomic.Uint64)
		_, root = p9fs.NewDirectory(
			commonOptions[p9fs.DirectoryOption](
				nil, "", path,
				uid, gid, permissions,
			)...,
		)
		system = fileSystem{
			root:    root,
			mount:   newMounter(root, path, uid, gid, permissions),
			listen:  newListener(ctx, root, path, uid, gid, permissions),
			control: newControl(ctx, root, path, uid, gid, permissions),
		}
	)
	return system, linkSystems(system)
}

func newMounter(parent p9.File, path ninePath,
	uid p9.UID, gid p9.GID, permissions p9.FileMode,
) mountSubsystem {
	const (
		mounterName = "mounts"
		autoUnlink  = true
	)
	var (
		makeHostFn = newHostFunc(path)
		_, mountFS = p9fs.NewMounter(
			makeHostFn,
			append(
				commonOptions[p9fs.MounterOption](
					parent, mounterName, path,
					uid, gid, permissions,
				),
				p9fs.UnlinkEmptyChildren[p9fs.MounterOption](autoUnlink),
			)...,
		)
	)
	return mountSubsystem{
		name:      mounterName,
		MountFile: mountFS,
	}
}

func newHostFunc(path ninePath) p9fs.MakeHostFunc {
	return func(parent p9.File, host filesystem.Host, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, p9.File, error) {
		var makeGuestFn p9fs.MakeGuestFunc
		switch host {
		case cgofuse.HostID:
			makeGuestFn = newGuestFunc[*cgofuse.Host](path)
		default:
			err := fmt.Errorf(`unexpected host "%v"`, host)
			return p9.QID{}, nil, err
		}
		var (
			name        = string(host)
			qid, hoster = p9fs.NewHostFile(
				makeGuestFn,
				append(
					commonOptions[p9fs.HosterOption](
						parent, name, path,
						uid, gid, permissions,
					),
					// TODO: values should come from caller.
					p9fs.UnlinkEmptyChildren[p9fs.HosterOption](true),
					p9fs.UnlinkWhenEmpty[p9fs.HosterOption](true),
				)...,
			)
		)
		return qid, hoster, nil
	}
}

func newGuestFunc[H mountHost[T], T any](path ninePath) p9fs.MakeGuestFunc {
	return func(parent p9.File, guest filesystem.ID, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, p9.File, error) {
		var (
			makeMountPointFn p9fs.MakeMountPointFunc
			options          = append(
				commonOptions[p9fs.FSIDOption](
					parent, string(guest), path,
					uid, gid, permissions,
				),
				// TODO: values should come from caller.
				p9fs.UnlinkEmptyChildren[p9fs.FSIDOption](true),
				p9fs.UnlinkWhenEmpty[p9fs.FSIDOption](true),
			)
		)
		// TODO: share IPFS instances
		// when server API is the same
		// (needs some wrapper too so
		// Close works properly.)
		switch guest {
		case ipfs.IPFSID:
			makeMountPointFn = newMountPointFunc[H, *ipfs.IPFSGuest](path)
		case ipfs.PinFSID:
			makeMountPointFn = newMountPointFunc[H, *ipfs.PinFSGuest](path)
		case ipfs.IPNSID:
			makeMountPointFn = newMountPointFunc[H, *ipfs.IPNSGuest](path)
		case ipfs.KeyFSID:
			makeMountPointFn = newMountPointFunc[H, *ipfs.KeyFSGuest](path)
		default:
			err := fmt.Errorf(`unexpected guest "%v"`, guest)
			return p9.QID{}, nil, err
		}
		qid, file := p9fs.NewGuestFile(makeMountPointFn, options...)
		return qid, file, nil
	}
}

func newMountPointFunc[
	H mountHost[HT],
	G mountGuest[GT],
	HT, GT any,
](path ninePath,
) p9fs.MakeMountPointFunc {
	return func(parent p9.File, name string, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, p9.File, error) {
		qid, file := p9fs.NewMountPoint[*mountPoint[HT, GT, H, G]](
			commonOptions[p9fs.MountPointOption](
				parent, name, path,
				uid, gid, permissions,
			)...,
		)
		return qid, file, nil
	}
}

func newListener(ctx context.Context, parent p9.File, path ninePath,
	uid p9.UID, gid p9.GID, permissions p9.FileMode,
) listenSubsystem {
	const name = "listeners"
	_, listenFS, listeners := p9fs.NewListener(ctx,
		append(
			commonOptions[p9fs.ListenerOption](
				parent, name, path,
				uid, gid, permissions,
			),
			p9fs.UnlinkEmptyChildren[p9fs.ListenerOption](true),
		)...,
	)
	return listenSubsystem{
		name:      name,
		Listener:  listenFS,
		listeners: listeners,
	}
}

func newControl(ctx context.Context,
	parent p9.File, path ninePath,
	uid p9.UID, gid p9.GID, permissions p9.FileMode,
) controlSubsystem {
	const (
		controlName  = "control"
		shutdownName = "shutdown"
	)
	var (
		_, control = p9fs.NewDirectory(
			commonOptions[p9fs.DirectoryOption](parent, controlName, path, uid, gid, permissions)...,
		)
		_, shutdownFile, shutdownCh = p9fs.NewChannelFile(ctx,
			commonOptions[p9fs.ChannelOption](control, shutdownName, path, uid, gid, permissions)...,
		)
	)
	if err := control.Link(shutdownFile, shutdownName); err != nil {
		panic(err)
	}
	return controlSubsystem{
		name:      controlName,
		Directory: control,
		shutdown: shutdown{
			ChannelFile: shutdownFile,
			name:        shutdownName,
			ch:          shutdownCh,
		},
	}
}

func linkSystems(system fileSystem) error {
	root := system.root
	for _, file := range []struct {
		p9.File
		name string
	}{
		{
			name: system.mount.name,
			File: system.mount.MountFile,
		},
		{
			name: system.listen.name,
			File: system.listen.Listener,
		},
		{
			name: system.control.name,
			File: system.control.Directory,
		},
	} {
		if err := root.Link(file.File, file.name); err != nil {
			return err
		}
	}
	return nil
}

func makeAndStartServer(
	fsys fileSystem, log ulog.Logger,
) (*p9net.Server, <-chan error) {
	var (
		server = p9net.NewServer(fsys.root,
			p9net.WithServerLogger(log),
		)
		listenSys = fsys.listen
	)
	return server, logAndServeListeners(listenSys, server, log)
}

func logAndServeListeners(system listenSubsystem,
	server *p9net.Server, log ulog.Logger,
) <-chan error {
	var (
		listeners = system.listeners
		serveFn   = server.Serve
	)
	if log != nil &&
		log != ulog.Null {
		listeners = logListeners(log, listeners)
	}
	return serveListeners(serveFn, listeners)
}

func logListeners(log ulog.Logger, in <-chan manet.Listener,
) <-chan manet.Listener {
	out := make(chan manet.Listener, cap(in))
	go func() {
		defer close(out)
		for l := range in {
			log.Printf("listening on: %s\n", l.Multiaddr())
			out <- l
		}
	}()
	return out
}

func serveListeners(serveFn serveFunc,
	listeners <-chan manet.Listener,
) <-chan error {
	var (
		serveWg sync.WaitGroup
		errs    = make(chan error)
		serve   = func(listener manet.Listener) {
			defer serveWg.Done()
			err := serveFn(listener)
			if err == nil ||
				// Ignore value caused by server.Shutdown().
				// (daemon closed listener.)
				errors.Is(err, p9net.ErrServerClosed) ||
				// Ignore value caused by listener.Close().
				// (external|fs closed listener.)
				errors.Is(err, net.ErrClosed) {
				return
			}
			errs <- fmt.Errorf("%w: %s - %s",
				errServe, listener.Multiaddr(), err,
			)
		}
	)
	go func() {
		for listener := range listeners {
			serveWg.Add(1)
			go serve(listener)
		}
		serveWg.Wait()
		close(errs)
	}()
	return errs
}

func makeStopper(ctx context.Context, cancel context.CancelFunc,
	mountSys mountSubsystem, server *p9net.Server,
	log ulog.Logger,
) (stopperWrite, <-chan error) {
	var (
		stopper      = make(stopperChan, maximumShutdown)
		sRelay       = make(stopperChan, maximumShutdown)
		mRelay       = make(stopperChan, 1)
		shutdownErrs = shutdownWith(ctx, sRelay, server, log)
		closeErrs    = closeWith(mRelay, mountSys, log)
		errs         = make(chan error)
	)
	// Listen for signals, and stop in sequence.
	// Always stop the network first;
	// then when it's done, stop the mount subsystem.
	// In the future these might be done concurrently.
	// As-is we just don't want an active connection
	// to be mucking with the system during shutdown.
	// Tearing down connections first, loosely assures
	// we're the only caller with a valid handle to the fs.
	go func() {
		defer func() {
			cancel()
			close(errs)
		}()
		var highestSeen shutdownDisposition
		for shutdownErrs != nil ||
			closeErrs != nil {
			select {
			case level := <-stopper:
				if level > highestSeen {
					highestSeen = level
					sRelay <- level
				}
			case err, ok := <-shutdownErrs:
				if nilClosedChan(&shutdownErrs, ok) {
					mRelay <- patientShutdown
					continue
				}
				errs <- err
			case err, ok := <-closeErrs:
				if nilClosedChan(&closeErrs, ok) {
					continue
				}
				errs <- err
			}
		}
	}()
	return stopper, errs
}

func nilClosedChan[T any](chPtr *<-chan T, ok bool) bool {
	if !ok {
		*chPtr = nil
	}
	return !ok
}

func shutdownWith(ctx context.Context, levels stopperRead,
	server *p9net.Server, log ulog.Logger,
) <-chan error {
	const (
		waitMsg = "closing listeners now" +
			" and connections when they're idle"
		msgPrefix = "closing connections"
		deadline  = 10 * time.Second
		nowMsg    = msgPrefix + " immediately"

		waitForConns = patientShutdown
		timeoutConns = shortShutdown
		closeConns   = immediateShutdown
	)
	var (
		sCtx, cancel = context.WithCancel(ctx)
		errs         = make(chan error)
		once         sync.Once
		shutdownFn   = server.Shutdown
		shutdown     = func() {
			go func() {
				if err := shutdownFn(sCtx); err != nil {
					errs <- err
				}
				cancel()
				close(errs)
			}()
		}
	)
	go func() {
		for level := range levels {
			once.Do(shutdown)
			switch level {
			case waitForConns:
				log.Print(waitMsg)
			case timeoutConns:
				time.AfterFunc(deadline, cancel)
				log.Printf("%s in %s", msgPrefix, deadline)
			case closeConns:
				cancel()
				log.Print(nowMsg)
			}
		}
		close(errs)
	}()
	return errs
}

func closeWith(levels stopperRead,
	system mountSubsystem, log ulog.Logger,
) <-chan error {
	var (
		dir  = system.MountFile
		errs = make(chan error)
	)
	go func() {
		<-levels
		log.Print("closing mounts")
		if err := p9fs.UnmountAll(dir); err != nil {
			errs <- err
		}
		close(errs)
	}()
	return errs
}

func relayOSSignal(ctx context.Context, sig os.Signal, stopper chan<- shutdownDisposition) {
	signals := make(chan os.Signal, cap(stopper))
	signal.Notify(signals, sig)
	go func() {
		defer signal.Stop(signals)
		for count := minimumShutdown; count <= maximumShutdown; count++ {
			select {
			case <-signals:
				select {
				case stopper <- count:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func watchCtx(ctx context.Context, stopper stopperWrite) {
	go func() { <-ctx.Done(); stopper <- immediateShutdown }()
}

func watchShutdown(ctx context.Context,
	data <-chan []byte, stopper stopperWrite, log ulog.Logger,
) <-chan error {
	errs := make(chan error)
	go func() {
		defer close(errs)
		for {
			select {
			case data, ok := <-data:
				if !ok {
					return
				}
				level, err := parseDispositionData(data)
				if err != nil {
					select {
					case errs <- err:
						continue
					case <-ctx.Done():
						return
					}
				}
				log.Print("external source requested to shutdown")
				select {
				case stopper <- level:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return errs
}

func parseDispositionData(data []byte) (sd shutdownDisposition, err error) {
	const expectedSize = int(unsafe.Sizeof(sd))
	if len(data) != expectedSize {
		err = fmt.Errorf("%w:"+
			" data is not expected size"+
			" got: %d, want: %d",
			errShutdownDisposition, sd, expectedSize,
		)
		return
	}
	type intent = shutdownDisposition
	if sd = intent(data[0]); sd > maximumShutdown {
		err = fmt.Errorf("%w:"+
			"got: %d, max level is: %d",
			errShutdownDisposition, sd, maximumShutdown,
		)
	}
	return
}

func isPipe(file *os.File) bool {
	fStat, err := file.Stat()
	if err != nil {
		return false
	}
	return fStat.Mode().Type()&os.ModeNamedPipe != 0
}

func handleStdio(handleFn handleFunc) <-chan error {
	errs := make(chan error)
	go func() {
		defer close(errs)
		if err := handleFn(os.Stdin, os.Stdout); err != nil {
			if !errors.Is(err, io.EOF) {
				errs <- err
				return
			}
		}
		if err := os.Stderr.Close(); err != nil {
			errs <- err
		}
	}()
	return errs
}

func stopWhen(ctx context.Context, interval time.Duration,
	stopper stopperWrite, checkFn checkFunc,
) <-chan error {
	errs := make(chan error)
	go func() {
		defer close(errs)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				stop, level, err := checkFn()
				if err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
					}
					return
				}
				if stop {
					select {
					case stopper <- level:
					case <-ctx.Done():
					}
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return errs
}

func makeMountChecker(fsys p9.File, log ulog.Logger) checkFunc {
	return func() (stop bool, sd shutdownDisposition, err error) {
		var mounted bool
		if mounted, err = hasEntries(fsys); err != nil {
			return
		}
		if !mounted {
			log.Print("no active mounts - requesting idle shutdown")
			stop = true
			sd = minimumShutdown
		}
		return
	}
}

func hasEntries(fsys p9.File) (bool, error) {
	ents, err := p9fs.ReadDir(fsys)
	if err != nil {
		return false, err
	}
	return len(ents) > 0, nil
}

// makeEmptyChecker prevents the process from lingering around
// if a client closes all services, then disconnects.
func makeEmptyChecker(systems *daemonSystem, log ulog.Logger) checkFunc {
	var (
		mountSys     = systems.mount
		listenersSys = systems.listen
	)
	return func() (stop bool, sd shutdownDisposition, err error) {
		var (
			mounted, mErr   = hasEntries(mountSys)
			listening, lErr = hasEntries(listenersSys)
		)
		if err = fserrors.Join(mErr, lErr); err != nil {
			return
		}
		if mounted || listening {
			return
		}
		log.Print("daemon is idle and not reachable" +
			" - requesting shutdown")
		stop = true
		sd = minimumShutdown
		return
	}
}

func aggregate[T any](sources ...<-chan T) []T {
	var (
		sourceCount = len(sources)
		cases       = make([]reflect.SelectCase, sourceCount)
		out         = make([]T, 0)
		disable     = reflect.Value{}
	)
	for i, source := range sources {
		cases[i] = reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(source),
			Send: disable,
		}
	}
	for remaining := sourceCount; remaining != 0; {
		chosen, value, ok := reflect.Select(cases)
		if !ok {
			cases[chosen].Chan = disable
			remaining--
			continue
		}
		out = append(out, value.Interface().(T))
	}
	return out
}
