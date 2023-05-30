package commands

import (
	"context"
	"encoding/csv"
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

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
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
		serverMaddrs []multiaddr.Multiaddr
		exitInterval time.Duration
		nineIDs
		commonSettings
		permissions fs.FileMode
	}
	nineIDs struct {
		uid p9.UID
		gid p9.GID
	}
	ninePath   = *atomic.Uint64
	fileSystem struct {
		path    ninePath
		root    p9.File
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
		cancel    context.CancelFunc
		name      string
	}
	controlSubsystem struct {
		directory p9.File
		name      string
		shutdown
	}
	shutdown struct {
		*p9fs.ChannelFile
		ch     <-chan []byte
		cancel context.CancelFunc
		name   string
	}
	daemonSystem struct {
		log   ulog.Logger
		files fileSystem
	}
	handleFunc = func(io.ReadCloser, io.WriteCloser) error
	serveFunc  = func(manet.Listener) error
	checkFunc  = func() (bool, shutdownDisposition, error)

	mountHost[T any] interface {
		*T
		p9fs.FieldParser
		p9fs.Mounter
		p9fs.HostIdentifier
	}
	mountGuest[T any] interface {
		*T
		p9fs.FieldParser
		p9fs.SystemMaker
		p9fs.GuestIdentifier
	}
	mountPoint[
		HT, GT any,
		H mountHost[HT],
		G mountGuest[GT],
	] struct {
		Host  HT `json:"host"`
		Guest GT `json:"guest"`
	}
	waitGroupChan[T any] struct {
		ch      chan T
		closing chan struct{}
		sync.WaitGroup
	}
	wgErrs     = *waitGroupChan[error]
	wgShutdown = *waitGroupChan[shutdownDisposition]
)

const (
	daemonCommandName = "daemon"

	errServe               = generic.ConstError("encountered error while serving")
	errShutdownDisposition = generic.ConstError("invalid shutdown disposition")
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

func (mp *mountPoint[HT, GT, H, G]) HostID() filesystem.Host {
	return H(&mp.Host).HostID()
}

func (mp *mountPoint[HT, GT, H, G]) GuestID() filesystem.ID {
	return G(&mp.Guest).GuestID()
}

func (set *daemonSettings) BindFlags(flagSet *flag.FlagSet) {
	set.commonSettings.BindFlags(flagSet)
	const (
		sockName  = serverFlagName
		sockUsage = "listening socket `maddr`" +
			"\ncan be specified multiple times and/or comma separated" +
			"\n\b" // Newline for default value, sans space.
	)
	var (
		sockFlagSet     bool
		sockDefaultText string
	)
	{
		maddrs, err := userServiceMaddrs()
		if err != nil {
			panic(err)
		}
		sockDefault := maddrs[0]
		sockDefaultText = sockDefault.String()
		set.serverMaddrs = maddrs[:1]
	}
	flagSet.Func(sockName, sockUsage, func(s string) error {
		maddrStrings, err := csv.NewReader(strings.NewReader(s)).Read()
		if err != nil {
			return err
		}
		for _, maddrString := range maddrStrings {
			maddr, err := multiaddr.NewMultiaddr(maddrString)
			if err != nil {
				return err
			}
			if !sockFlagSet {
				// Don't append to the default value(s).
				set.serverMaddrs = []multiaddr.Multiaddr{maddr}
				sockFlagSet = true
				continue
			}
			set.serverMaddrs = append(set.serverMaddrs, maddr)
		}
		return nil
	})
	const (
		exitFlag  = exitAfterFlagName
		exitUsage = "check every `interval` (e.g. \"30s\") and shutdown the daemon if its idle"
	)
	flagSet.DurationVar(&set.exitInterval, exitFlag, 0, exitUsage)
	const (
		uidName        = apiFlagPrefix + "uid"
		uidDefaultText = "nobody"
		uidUsage       = "file owner's `uid`"
	)
	set.uid = p9.NoUID
	flagSet.Func(uidName, uidUsage, func(s string) (err error) {
		set.uid, err = parseID[p9.UID](s)
		return
	})
	const (
		gidName        = apiFlagPrefix + "gid"
		gidDefaultText = "nobody"
		gidUsage       = "file owner's `gid`"
	)
	set.gid = p9.NoGID
	flagSet.Func(gidName, gidUsage, func(s string) (err error) {
		set.gid, err = parseID[p9.GID](s)
		return
	})
	const (
		permissionsName    = apiFlagPrefix + "permissions"
		permissionsUsage   = "`permissions` to use when creating service files"
		permissionsDefault = 0o751
	)
	permissionsDefaultText := modeToSymbolicPermissions(
		fs.FileMode(permissionsDefault &^ p9.FileModeMask),
	)
	set.permissions = permissionsDefault
	flagSet.Func(permissionsName, permissionsUsage, func(s string) (err error) {
		permissions, err := parsePOSIXPermissions(set.permissions, s)
		if err != nil {
			return err
		}
		set.permissions = permissions &^ fs.ModeType
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
		name     = daemonCommandName
		synopsis = "Host system services."
		usage    = "File system service daemon."
	)
	return command.MustMakeCommand[*daemonSettings](name, synopsis, usage, daemonExecute)
}

func daemonExecute(ctx context.Context, set *daemonSettings) error {
	dCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	system, err := newSystem(dCtx, set)
	if err != nil {
		return err
	}
	const errBuffer = 0
	var (
		fsys   = system.files
		path   = fsys.path
		root   = fsys.root
		log    = system.log
		server = makeServer(newAttacher(path, root), log)
		stopSend,
		stopReceive = makeStoppers(ctx)
		lsnStop,
		srvStop,
		mntStop = splitStopper(stopReceive)
		listenSys = fsys.listen
		listeners = listenSys.listeners
		errs      = newWaitGroupChan[error](errBuffer)
	)
	handleListeners(server.Serve, listeners, errs, log)
	go watchListenersStopper(listenSys.cancel, lsnStop, log)
	serviceWg := handleStopSequence(dCtx,
		server, srvStop,
		fsys.mount, mntStop,
		errs, log,
	)
	var (
		listener    = listenSys.Listener
		permissions = modeFromFS(set.permissions)
		procExitCh  = listenOn(listener, permissions,
			stopSend, errs,
			set.serverMaddrs...,
		)
		control  = fsys.control.directory
		handleFn = server.Handle
	)
	setupIPCHandler(dCtx, procExitCh,
		control, handleFn,
		serviceWg, errs,
	)
	idleCheckInterval := set.exitInterval
	setupExtraStopWriters(idleCheckInterval, fsys,
		stopSend, errs,
		log,
	)
	return watchService(ctx, serviceWg,
		stopSend, errs,
		log,
	)
}

func watchService(ctx context.Context,
	serviceWg *sync.WaitGroup,
	stopSend wgShutdown, errs wgErrs,
	log ulog.Logger,
) error {
	go func() {
		serviceWg.Wait()
		stopSend.closeSend()
		stopSend.waitThenCloseCh()
		errs.waitThenCloseCh()
	}()
	var errSl []error
	for err := range errs.ch {
		log.Print(err)
		errSl = append(errSl, err)
	}
	if errSl != nil {
		return fmt.Errorf("daemon: %w", errors.Join(errSl...))
	}
	return ctx.Err()
}

func makeStoppers(ctx context.Context) (wgShutdown, <-chan shutdownDisposition) {
	var (
		shutdownChan   = newWaitGroupChan[shutdownDisposition](int(maximumShutdown))
		shutdownLevels = make(chan shutdownDisposition)
	)
	shutdownChan.Add(2)
	go stopOnSignal(os.Interrupt, shutdownChan)
	go stopOnDone(ctx, shutdownChan)
	go func() {
		sequentialLeveling(shutdownChan.ch, shutdownLevels)
		close(shutdownLevels)
	}()
	return shutdownChan, shutdownLevels
}

func makeServer(fsys p9.Attacher, log ulog.Logger) *p9net.Server {
	server := p9net.NewServer(fsys,
		p9net.WithServerLogger(log),
	)
	return server
}

func splitStopper(shutdownLevels <-chan shutdownDisposition) (_, _, _ <-chan shutdownDisposition) {
	var lsnShutdownSignals,
		srvShutdownSignals,
		mntShutdownSignals <-chan shutdownDisposition
	relayUnordered(shutdownLevels, &lsnShutdownSignals,
		&srvShutdownSignals, &mntShutdownSignals)
	return lsnShutdownSignals, srvShutdownSignals, mntShutdownSignals
}

func handleListeners(serveFn serveFunc,
	listeners <-chan manet.Listener, errs wgErrs,
	log ulog.Logger,
) {
	if log != nil &&
		log != ulog.Null {
		var listenersDuplicate,
			listenersLog <-chan manet.Listener
		relayUnordered(listeners,
			&listenersDuplicate, &listenersLog)
		listeners = listenersDuplicate
		go logListeners(log, listenersLog)
	}
	errs.Add(1)
	go serveListeners(serveFn, listeners, errs)
}

func handleStopSequence(ctx context.Context,
	server *p9net.Server, srvStop <-chan shutdownDisposition,
	mount mountSubsystem, mntStop <-chan shutdownDisposition,
	errs wgErrs, log ulog.Logger,
) *sync.WaitGroup {
	var serverWg,
		mountWg sync.WaitGroup
	errs.Add(2)
	serverWg.Add(1)
	mountWg.Add(1)
	go func() {
		defer serverWg.Done()
		serverStopper(ctx, server, srvStop, errs, log)
	}()
	go func() {
		serverWg.Wait()
		unmountAll(mount, mntStop, errs, log)
		mountWg.Done()
	}()
	return &mountWg
}

func listenOn(listener *p9fs.Listener, permissions p9.FileMode,
	stopper wgShutdown,
	errs wgErrs,
	maddrs ...multiaddr.Multiaddr,
) <-chan bool {
	var (
		wg           sync.WaitGroup
		sawError     atomic.Bool
		processMaddr = func(maddr multiaddr.Multiaddr) {
			defer func() { wg.Done(); stopper.Done(); errs.Done() }()
			err := p9fs.Listen(listener, maddr, permissions)
			if err != nil {
				errs.send(fmt.Errorf(
					"could not listen on: %s - %w",
					maddr, err,
				))
				stopper.send(patientShutdown)
				sawError.Store(true)
			}
		}
		maddrCount = len(maddrs)
	)
	wg.Add(maddrCount)
	stopper.Add(maddrCount)
	errs.Add(maddrCount)
	for _, maddr := range maddrs {
		go processMaddr(maddr)
	}
	failureSignal := make(chan bool, 1)
	go func() {
		defer close(failureSignal)
		wg.Wait()
		if sawError.Load() {
			failureSignal <- true
		}
	}()
	return failureSignal
}

func setupIPCHandler(ctx context.Context, procExitCh <-chan bool,
	control p9.File, handlerFn handleFunc,
	serviceWg *sync.WaitGroup, errs wgErrs,
) {
	if !isPipe(os.Stdin) {
		return
	}
	serviceWg.Add(1)
	errs.Add(1)
	go handleStdio(ctx, procExitCh,
		control, handlerFn,
		serviceWg, errs,
	)
}

func setupExtraStopWriters(
	idleCheck time.Duration, fsys fileSystem,
	stopper wgShutdown,
	errs wgErrs, log ulog.Logger,
) {
	shutdownFileData := fsys.control.shutdown.ch
	stopper.Add(2)
	errs.Add(2)
	go stopOnUnreachable(fsys, stopper, errs, log)
	go stopOnShutdownWrite(shutdownFileData, stopper, errs, log)
	if idleCheck != 0 {
		stopper.Add(1)
		errs.Add(1)
		idleCheckFn := makeIdleChecker(fsys, idleCheck, log)
		go stopWhen(idleCheckFn, idleCheck, stopper, errs)
	}
}

func newWaitGroupChan[T any](size int) *waitGroupChan[T] {
	return &waitGroupChan[T]{
		ch:      make(chan T),
		closing: make(chan struct{}, size),
	}
}

func (wc *waitGroupChan[T]) Closing() <-chan struct{} {
	return wc.closing
}

func (wc *waitGroupChan[T]) closeSend() {
	close(wc.closing)
}

func (wc *waitGroupChan[T]) send(value T) (sent bool) {
	select {
	case wc.ch <- value:
		sent = true
	case <-wc.closing:
	}
	return sent
}

func (wc *waitGroupChan[T]) waitThenCloseCh() {
	wc.WaitGroup.Wait()
	close(wc.ch)
}

func newSystem(ctx context.Context, set *daemonSettings) (*daemonSystem, error) {
	var (
		uid       = set.uid
		gid       = set.gid
		fsys, err = newFileSystem(ctx, uid, gid)
		system    = &daemonSystem{
			files: fsys,
			log:   newDaemonLog(set.verbose),
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

func unlinkEmptyDirs[OT p9fs.DirectoryOptions](autoUnlink bool) []OT {
	return []OT{
		p9fs.UnlinkEmptyChildren[OT](autoUnlink),
		p9fs.UnlinkWhenEmpty[OT](autoUnlink),
	}
}

func newFileSystem(ctx context.Context, uid p9.UID, gid p9.GID) (fileSystem, error) {
	const permissions = p9fs.ReadUser | p9fs.WriteUser | p9fs.ExecuteUser |
		p9fs.ReadGroup | p9fs.ExecuteGroup |
		p9fs.ReadOther | p9fs.ExecuteOther
	var (
		path         = new(atomic.Uint64)
		_, root, err = p9fs.NewDirectory(append(
			commonOptions[p9fs.DirectoryOption](
				nil, "", path,
				uid, gid, permissions,
			),
			p9fs.WithoutRename[p9fs.DirectoryOption](true),
		)...,
		)
	)
	if err != nil {
		return fileSystem{}, err
	}
	mount, err := newMounter(root, path, uid, gid, permissions)
	if err != nil {
		return fileSystem{}, err
	}
	listen, err := newListener(ctx, root, path, uid, gid, permissions)
	if err != nil {
		return fileSystem{}, err
	}
	control, err := newControl(ctx, root, path, uid, gid, permissions)
	if err != nil {
		return fileSystem{}, err
	}
	system := fileSystem{
		path:    path,
		root:    root,
		mount:   mount,
		listen:  listen,
		control: control,
	}
	return system, linkSystems(system)
}

func newMounter(parent p9.File, path ninePath,
	uid p9.UID, gid p9.GID, permissions p9.FileMode,
) (mountSubsystem, error) {
	const autoUnlink = true
	var (
		makeHostFn = newHostFunc(path, autoUnlink)
		options    = append(
			commonOptions[p9fs.MounterOption](
				parent, mountsFileName, path,
				uid, gid, permissions,
			),
			p9fs.UnlinkEmptyChildren[p9fs.MounterOption](autoUnlink),
			p9fs.WithoutRename[p9fs.MounterOption](true),
		)
		_, mountFS, err = p9fs.NewMounter(makeHostFn, options...)
	)
	if err != nil {
		return mountSubsystem{}, err
	}
	return mountSubsystem{
		name:      mountsFileName,
		MountFile: mountFS,
	}, nil
}

func mountsDirCreatePreamble(mode p9.FileMode) (p9.FileMode, error) {
	if !mode.IsDir() {
		return 0, generic.ConstError("expected to be called from mkdir")
	}
	return mode.Permissions(), nil
}

func newHostFunc(path ninePath, autoUnlink bool) p9fs.MakeHostFunc {
	linkOptions := unlinkEmptyDirs[p9fs.HosterOption](autoUnlink)
	return func(parent p9.File, host filesystem.Host, mode p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, p9.File, error) {
		permissions, err := mountsDirCreatePreamble(mode)
		if err != nil {
			return p9.QID{}, nil, err
		}
		var makeGuestFn p9fs.MakeGuestFunc
		switch host {
		case cgofuse.HostID:
			makeGuestFn = newGuestFunc[*cgofuse.Host](path, autoUnlink)
		default:
			err := fmt.Errorf(`unexpected host "%v"`, host)
			return p9.QID{}, nil, err
		}
		var (
			name    = string(host)
			options = append(
				commonOptions[p9fs.HosterOption](
					parent, name, path,
					uid, gid, permissions,
				),
				linkOptions...,
			)
		)
		options = append(options,
			p9fs.WithoutRename[p9fs.HosterOption](true),
		)
		return p9fs.NewHostFile(makeGuestFn, options...)
	}
}

func newGuestFunc[H mountHost[T], T any](path ninePath, autoUnlink bool) p9fs.MakeGuestFunc {
	linkOptions := unlinkEmptyDirs[p9fs.GuestOption](autoUnlink)
	return func(parent p9.File, guest filesystem.ID, mode p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, p9.File, error) {
		permissions, err := mountsDirCreatePreamble(mode)
		if err != nil {
			return p9.QID{}, nil, err
		}
		var (
			makeMountPointFn p9fs.MakeMountPointFunc
			options          = append(
				commonOptions[p9fs.GuestOption](
					parent, string(guest), path,
					uid, gid, permissions,
				),
				linkOptions...,
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
		return p9fs.NewGuestFile(makeMountPointFn, options...)
	}
}

func mountsFileCreatePreamble(mode p9.FileMode) (p9.FileMode, error) {
	if !mode.IsRegular() {
		return 0, generic.ConstError("expected to be called from mknod")
	}
	return mode.Permissions(), nil
}

func newMountPointFunc[
	H mountHost[HT],
	G mountGuest[GT],
	HT, GT any,
](path ninePath,
) p9fs.MakeMountPointFunc {
	return func(parent p9.File, name string, mode p9.FileMode, uid p9.UID, gid p9.GID) (p9.QID, p9.File, error) {
		permissions, err := mountsFileCreatePreamble(mode)
		if err != nil {
			return p9.QID{}, nil, err
		}
		return p9fs.NewMountPoint[*mountPoint[HT, GT, H, G]](
			commonOptions[p9fs.MountPointOption](
				parent, name, path,
				uid, gid, permissions,
			)...,
		)
	}
}

func newListener(ctx context.Context, parent p9.File, path ninePath,
	uid p9.UID, gid p9.GID, permissions p9.FileMode,
) (listenSubsystem, error) {
	lCtx, cancel := context.WithCancel(ctx)
	_, listenFS, listeners, err := p9fs.NewListener(lCtx, append(
		commonOptions[p9fs.ListenerOption](
			parent, listenersFileName, path,
			uid, gid, permissions,
		),
		p9fs.UnlinkEmptyChildren[p9fs.ListenerOption](true),
	)...,
	)
	if err != nil {
		cancel()
		return listenSubsystem{}, err
	}
	return listenSubsystem{
		name:      listenersFileName,
		Listener:  listenFS,
		listeners: listeners,
		cancel:    cancel,
	}, nil
}

func newControl(ctx context.Context,
	parent p9.File, path ninePath,
	uid p9.UID, gid p9.GID, permissions p9.FileMode,
) (controlSubsystem, error) {
	_, control, err := p9fs.NewDirectory(append(
		commonOptions[p9fs.DirectoryOption](parent, controlFileName, path, uid, gid, permissions),
		p9fs.WithoutRename[p9fs.DirectoryOption](true),
	)...,
	)
	if err != nil {
		return controlSubsystem{}, err
	}
	var (
		sCtx, cancel    = context.WithCancel(ctx)
		filePermissions = permissions ^ (p9fs.ExecuteOther | p9fs.ExecuteGroup | p9fs.ExecuteUser)
	)
	_, shutdownFile, shutdownCh, err := p9fs.NewChannelFile(sCtx,
		commonOptions[p9fs.ChannelOption](control, shutdownFileName, path, uid, gid, filePermissions)...,
	)
	if err != nil {
		cancel()
		return controlSubsystem{}, err
	}
	if err := control.Link(shutdownFile, shutdownFileName); err != nil {
		cancel()
		return controlSubsystem{}, err
	}
	return controlSubsystem{
		name:      controlFileName,
		directory: control,
		shutdown: shutdown{
			ChannelFile: shutdownFile,
			name:        shutdownFileName,
			ch:          shutdownCh,
			cancel:      cancel,
		},
	}, nil
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
			File: system.control.directory,
		},
	} {
		if err := root.Link(file.File, file.name); err != nil {
			return err
		}
	}
	return nil
}

func logListeners(log ulog.Logger, listeners <-chan manet.Listener) {
	for l := range listeners {
		log.Printf("listening on: %s\n", l.Multiaddr())
	}
}

func serveListeners(serveFn serveFunc, listeners <-chan manet.Listener,
	errs wgErrs,
) {
	defer errs.Done()
	var (
		serveWg sync.WaitGroup
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
			err = fmt.Errorf("%w: %s - %s",
				errServe, listener.Multiaddr(), err,
			)
			errs.send(err)
		}
	)
	for listener := range listeners {
		serveWg.Add(1)
		go serve(listener)
	}
	serveWg.Wait()
}

func relayUnordered[T any](in <-chan T, outs ...*<-chan T) {
	chs := make([]chan<- T, len(outs))
	for i := range outs {
		ch := make(chan T, cap(in))
		*outs[i] = ch
		chs[i] = ch
	}
	go relayChan(in, chs...)
}

// relayChan will relay values (in a non-blocking manner)
// from `source` to all `relays` (immediately or eventually).
// The source must be closed to stop processing.
// Each relay is closed after all values are sent.
// Relay receive order is not guaranteed to match
// source's order.
func relayChan[T any](source <-chan T, relays ...chan<- T) {
	var (
		relayValues  = reflectSendChans(relays...)
		relayCount   = len(relayValues)
		disabledCase = reflect.Value{}
		defaultCase  = relayCount
		cases        = make([]reflect.SelectCase, defaultCase+1)
		closerWgs    = make([]*sync.WaitGroup, relayCount)
		send         = func(wg *sync.WaitGroup, ch chan<- T, value T) {
			ch <- value
			wg.Done()
		}
	)
	cases[defaultCase] = reflect.SelectCase{Dir: reflect.SelectDefault}
	for value := range source {
		populateSelectSendCases(value, relayValues, cases)
		for remaining := relayCount; remaining != 0; {
			chosen, _, _ := reflect.Select(cases)
			if chosen != defaultCase {
				cases[chosen].Chan = disabledCase
				remaining--
				continue
			}
			for i, commCase := range cases[:relayCount] {
				if !commCase.Chan.IsValid() {
					continue // Already sent.
				}
				wg := closerWgs[i]
				if wg == nil {
					wg = new(sync.WaitGroup)
					closerWgs[i] = wg
				}
				wg.Add(1)
				go send(wg, relays[i], value)
			}
			break
		}
	}
	waitAndClose := func(wg *sync.WaitGroup, ch chan<- T) {
		wg.Wait()
		close(ch)
	}
	for i, wg := range closerWgs {
		if wg == nil {
			close(relays[i])
			continue
		}
		go waitAndClose(wg, relays[i])
	}
}

func reflectSendChans[T any](chans ...chan<- T) []reflect.Value {
	values := make([]reflect.Value, len(chans))
	for i, relay := range chans {
		values[i] = reflect.ValueOf(relay)
	}
	return values
}

// populateSelectSendCases will create
// send cases containing `value` for
// each channel in `channels`, and assign it
// within `cases`. Panics if len(cases) < len(channels).
func populateSelectSendCases[T any](value T, channels []reflect.Value, cases []reflect.SelectCase) {
	rValue := reflect.ValueOf(value)
	for i, channel := range channels {
		cases[i] = reflect.SelectCase{
			Dir:  reflect.SelectSend,
			Chan: channel,
			Send: rValue,
		}
	}
}

func sequentialLeveling(stopper <-chan shutdownDisposition, filtered chan<- shutdownDisposition) {
	var highestSeen shutdownDisposition
	for level := range stopper {
		if level > highestSeen {
			highestSeen = level
			filtered <- level
		}
	}
}

func watchListenersStopper(cancel context.CancelFunc,
	stopper <-chan shutdownDisposition, log ulog.Logger,
) {
	for range stopper {
		log.Print("stop signal received - not accepting new listeners")
		cancel()
		return
	}
}

func serverStopper(ctx context.Context,
	server *p9net.Server, stopper <-chan shutdownDisposition,
	errs wgErrs, log ulog.Logger,
) {
	defer errs.Done()
	const (
		deadline   = 10 * time.Second
		msgPrefix  = "stop signal received - "
		connPrefix = "closing connections"
		waitMsg    = msgPrefix + "closing listeners now" +
			" and connections when they're idle"
		nowMsg       = msgPrefix + connPrefix + " immediately"
		waitForConns = patientShutdown
		timeoutConns = shortShutdown
		closeConns   = immediateShutdown
	)
	var (
		initiated    bool
		shutdownErr  = make(chan error, 1)
		sCtx, cancel = context.WithCancel(ctx)
	)
	defer cancel()
	for {
		select {
		case level, ok := <-stopper:
			if !ok {
				return
			}
			switch level {
			case waitForConns:
				log.Print(waitMsg)
			case timeoutConns:
				time.AfterFunc(deadline, cancel)
				log.Printf("%sforcefully %s in %s",
					msgPrefix, connPrefix, deadline)
			case closeConns:
				cancel()
				log.Print(nowMsg)
			default:
				err := fmt.Errorf("unexpected signal: %v", level)
				errs.send(err)
				continue
			}
			if !initiated {
				initiated = true
				go func() { shutdownErr <- server.Shutdown(sCtx) }()
			}
		case err := <-shutdownErr:
			if err != nil &&
				!errors.Is(err, context.Canceled) {
				errs.send(err)
			}
			return
		}
	}
}

func unmountAll(system mountSubsystem,
	levels <-chan shutdownDisposition,
	errs wgErrs, log ulog.Logger,
) {
	defer errs.Done()
	<-levels
	log.Print("stop signal received - unmounting all")
	dir := system.MountFile
	if err := p9fs.UnmountAll(dir); err != nil {
		errs.send(err)
	}
}

func stopOnSignal(sig os.Signal, stopCh wgShutdown) {
	signals := make(chan os.Signal, generic.Max(1, cap(stopCh.ch)))
	signal.Notify(signals, sig)
	defer func() {
		signal.Stop(signals)
		stopCh.Done()
	}()
	for count := minimumShutdown; count <= maximumShutdown; count++ {
		select {
		case <-signals:
			if !stopCh.send(count) {
				return
			}
		case <-stopCh.Closing():
			return
		}
	}
}

func stopOnDone(ctx context.Context, stopCh wgShutdown) {
	defer stopCh.Done()
	select {
	case <-ctx.Done():
		stopCh.send(immediateShutdown)
	case <-stopCh.closing:
	}
}

func stopOnUnreachable(fsys fileSystem, stopper wgShutdown,
	errs wgErrs, log ulog.Logger,
) {
	const (
		keepRunning = false
		stopRunning = true
		interval    = 10 * time.Minute
		idleMessage = "daemon is unreachable and has" +
			" no active mounts - unreachable shutdown"
	)
	var (
		idleCheckFn        = makeIdleChecker(fsys, interval, ulog.Null)
		listeners          = fsys.listen.Listener
		unreachableCheckFn = func() (bool, shutdownDisposition, error) {
			shutdown, _, err := idleCheckFn()
			if !shutdown || err != nil {
				return keepRunning, dontShutdown, err
			}
			haveNetwork, err := hasEntries(listeners)
			if haveNetwork || err != nil {
				return keepRunning, dontShutdown, err
			}
			log.Print(idleMessage)
			return stopRunning, immediateShutdown, nil
		}
	)
	stopWhen(unreachableCheckFn, interval, stopper, errs)
}

func stopOnShutdownWrite(data <-chan []byte, stopper wgShutdown,
	errs wgErrs, log ulog.Logger,
) {
	defer errs.Done()
	defer stopper.Done()
	for {
		select {
		case data, ok := <-data:
			if !ok {
				return
			}
			level, err := parseDispositionData(data)
			if err != nil {
				errs.send(err)
				continue
			}
			log.Print("external source requested to shutdown")
			if !stopper.send(level) {
				return
			}
		case <-stopper.Closing():
			return
		}
	}
}

func parseDispositionData(data []byte) (shutdownDisposition, error) {
	if len(data) != 1 {
		str := strings.TrimSpace(string(data))
		return generic.ParseEnum(minimumShutdown, maximumShutdown, str)
	}
	level := shutdownDisposition(data[0])
	if level < minimumShutdown || level > maximumShutdown {
		return 0, fmt.Errorf("%w:"+
			"got: %d, valid level range is: %d:%d",
			errShutdownDisposition, level,
			minimumShutdown, maximumShutdown,
		)
	}
	return level, nil
}

func isPipe(file *os.File) bool {
	fStat, err := file.Stat()
	if err != nil {
		return false
	}
	return fStat.Mode().Type()&os.ModeNamedPipe != 0
}

func handleStdio(ctx context.Context, exitCh <-chan bool,
	control p9.File, handleFn handleFunc,
	wg *sync.WaitGroup, errs wgErrs,
) {
	defer func() { wg.Done(); errs.Done() }()
	childProcInit()
	if exiting := <-exitCh; exiting {
		// Process wants to exit. Parent process
		return // should be monitoring stderr.
	}
	var (
		releaseCtx, cancel = context.WithCancel(ctx)
		releaseChan, err   = addIPCReleaseFile(releaseCtx, control)
	)
	if err != nil {
		cancel()
		errs.send(err)
		return
	}
	go func() {
		// NOTE:
		// 1) If we receive data, the parent process
		// is signaling that it's about to close the
		// write end of stderr. We don't validate this
		// because we'll be in a detached state. I.e.
		// even if we ferry the errors back to execute,
		// the write end of stderr is (likely) closed.
		// 2) If the parent process doesn't follow
		// our IPC protocol, this routine will remain
		// active. We don't force the service to wait
		// for our return; it's allowed to halt regardless.
		select {
		case <-releaseChan:
			defer cancel()
			const flags = 0
			// XXX: [presumption / no guard]
			// we assume no os handle access or changes
			// will happen during this window. Our only
			// writer should be in the return from main,
			// and daemon's execute should not be doing
			// (other) os file operations at this time.
			os.Stderr.Close()
			if discard, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
				os.Stderr = discard
			}
			control.UnlinkAt(ipcReleaseFileName, flags)
		case <-releaseCtx.Done():
		}
	}()
	if err := handleFn(os.Stdin, os.Stdout); err != nil {
		if !errors.Is(err, io.EOF) {
			errs.send(err)
			return
		}
		// NOTE: handleFn implicitly closes its parameters
		// before returning. Otherwise we'd close them.
	}
}

func addIPCReleaseFile(ctx context.Context, control p9.File) (<-chan []byte, error) {
	_, releaseFile, releaseChan, err := p9fs.NewChannelFile(ctx)
	if err != nil {
		return nil, err
	}
	if err := control.Link(releaseFile, ipcReleaseFileName); err != nil {
		return nil, err
	}
	return releaseChan, nil
}

func stopWhen(checkFn checkFunc, interval time.Duration,
	stopper wgShutdown,
	errs wgErrs,
) {
	defer func() {
		errs.Done()
		stopper.Done()
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			stop, level, err := checkFn()
			if err != nil {
				errs.send(err)
				return
			}
			if stop {
				stopper.send(level)
				return
			}
		case <-stopper.Closing():
			return
		}
	}
}

// makeIdleChecker prevents the process from lingering around
// if a client closes all services, then disconnects.
func makeIdleChecker(fsys fileSystem, interval time.Duration, log ulog.Logger) checkFunc {
	var (
		mounts    = fsys.mount.MountFile
		listeners = fsys.listen.Listener
	)
	const (
		keepRunning = false
		stopRunning = true
		idleMessage = "daemon has no active mounts or connections" +
			" - idle shutdown"
	)
	return func() (bool, shutdownDisposition, error) {
		mounted, err := hasEntries(mounts)
		if mounted || err != nil {
			return keepRunning, dontShutdown, err
		}
		activeConns, err := hasActiveClients(listeners, interval)
		if activeConns || err != nil {
			return keepRunning, dontShutdown, err
		}
		log.Print(idleMessage)
		return stopRunning, immediateShutdown, nil
	}
}

func hasEntries(fsys p9.File) (bool, error) {
	ents, err := p9fs.ReadDir(fsys)
	if err != nil {
		return false, err
	}
	return len(ents) > 0, nil
}

func hasActiveClients(listeners p9.File, threshold time.Duration) (bool, error) {
	infos, err := p9fs.GetConnections(listeners)
	if err != nil {
		return false, err
	}
	for _, info := range infos {
		lastActive := lastActive(info)
		if time.Since(lastActive) <= threshold {
			return true, nil
		}
	}
	return false, nil
}

func lastActive(info p9fs.ConnInfo) time.Time {
	var (
		read  = info.LastRead
		write = info.LastWrite
	)
	if read.After(write) {
		return read
	}
	return write
}
