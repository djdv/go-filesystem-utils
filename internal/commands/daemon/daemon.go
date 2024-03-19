package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/p9/p9"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/u-root/uio/ulog"
)

type (
	settings struct {
		serverMaddrs []multiaddr.Multiaddr
		exitInterval time.Duration
		nineIDs
		permissions            p9.FileMode
		logSystem, logProtocol bool
	}
	Option  func(*settings) error
	nineIDs struct {
		uid p9.UID
		gid p9.GID
	}
	ninePath       = *atomic.Uint64
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
	system struct {
		log   ulog.Logger
		files fileSystem
	}
	handleFunc = func(io.ReadCloser, io.WriteCloser) error
	checkFunc  = func() (bool, ShutdownDisposition, error)
)

const (
	DefaultUID         = p9.NoUID
	DefaultGID         = p9.NoGID
	DefaultPermissions = p9fs.ReadUser | p9fs.WriteUser | p9fs.ExecuteUser |
		p9fs.ReadGroup | p9fs.ExecuteGroup |
		p9fs.ExecuteOther
	DefaultExitIntervale time.Duration = 0

	errServe               = generic.ConstError("encountered error while serving")
	errShutdownDisposition = generic.ConstError("invalid shutdown disposition")
)

func DefaultAPIMaddr() multiaddr.Multiaddr {
	serviceMaddrs, err := allServiceMaddrs()
	if err != nil {
		panic(err)
	}
	return serviceMaddrs[0]
}

// Run starts the service, handles requests, and waits for a stop request.
func Run(ctx context.Context, options ...Option) error {
	settings, err := makeSettings(options...)
	if err != nil {
		return err
	}
	dCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	log, protoLog := makeLogs(&settings)
	system, err := newSystem(dCtx, settings.nineIDs, log)
	if err != nil {
		return err
	}
	const errBuffer = 0
	var (
		fsys   = system.files
		path   = fsys.path
		root   = fsys.root
		server = makeServer(
			newAttacher(path, root),
			protoLog,
		)
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
		permissions = settings.permissions
		procExitCh  = listenOn(listener, permissions,
			stopSend, errs,
			settings.serverMaddrs...,
		)
		control  = fsys.control.directory
		handleFn = server.Handle
	)
	setupIPCHandler(dCtx, procExitCh,
		control, handleFn,
		serviceWg, errs,
	)
	idleCheckInterval := settings.exitInterval
	setupExtraStopWriters(idleCheckInterval, &fsys,
		stopSend, errs,
		log,
	)
	return watchService(ctx, serviceWg,
		stopSend, errs,
		log,
	)
}

func makeSettings(options ...Option) (settings, error) {
	settings := settings{
		nineIDs: nineIDs{
			uid: DefaultUID,
			gid: DefaultGID,
		},
		permissions:  DefaultPermissions,
		exitInterval: DefaultExitIntervale,
	}
	if err := generic.ApplyOptions(&settings, options...); err != nil {
		return settings, err
	}
	if settings.serverMaddrs == nil {
		userMaddrs, err := allServiceMaddrs()
		if err != nil {
			return settings, err
		}
		settings.serverMaddrs = userMaddrs[0:1:1]
	}
	return settings, nil
}

func makeLogs(settings *settings) (system, protocol ulog.Logger) {
	if settings.logSystem {
		const (
			prefix = "daemon - "
			flags  = 0
		)
		system = log.New(os.Stderr, prefix, flags)
	} else {
		system = ulog.Null
	}
	if settings.logProtocol {
		const (
			prefix = "9P - "
			flags  = 0
		)
		protocol = log.New(os.Stderr, prefix, flags)
	} else {
		protocol = ulog.Null
	}
	return system, protocol
}

func WithMaddrs(maddrs ...multiaddr.Multiaddr) Option {
	return func(settings *settings) error {
		settings.serverMaddrs = maddrs
		return nil
	}
}

func WithExitInterval(interval time.Duration) Option {
	return func(settings *settings) error {
		settings.exitInterval = interval
		return nil
	}
}

// WithVerbosity enables all loggers.
func WithVerbosity(verbose bool) Option {
	return func(settings *settings) error {
		settings.logSystem = verbose
		settings.logProtocol = verbose
		return nil
	}
}

// WithSystemLog enables the daemon's message logger.
func WithSystemLog(verbose bool) Option {
	return func(settings *settings) error {
		settings.logSystem = verbose
		return nil
	}
}

// WithProtocolLog enables the 9P message logger.
func WithProtocolLog(verbose bool) Option {
	return func(settings *settings) error {
		settings.logProtocol = verbose
		return nil
	}
}

func WithUID(uid p9.UID) Option {
	return func(settings *settings) error {
		settings.uid = uid
		return nil
	}
}

func WithGID(gid p9.GID) Option {
	return func(settings *settings) error {
		settings.gid = gid
		return nil
	}
}

func WithPermissions(permissions p9.FileMode) Option {
	return func(settings *settings) error {
		settings.permissions = permissions
		return nil
	}
}

func newSystem(ctx context.Context, ids nineIDs, sysLog ulog.Logger) (*system, error) {
	var (
		uid       = ids.uid
		gid       = ids.gid
		fsys, err = newFileSystem(ctx, uid, gid)
		system    = &system{
			files: fsys,
			log:   sysLog,
		}
	)
	return system, err
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

func makeStoppers(ctx context.Context) (wgShutdown, <-chan ShutdownDisposition) {
	shutdownSend := newWaitGroupChan[ShutdownDisposition](int(maximumShutdown))
	registerSystemStoppers(ctx, shutdownSend)
	shutdownSend.Add(1)
	go stopOnDone(ctx, shutdownSend)
	shutdownReceive := make(chan ShutdownDisposition)
	go func() {
		sequentialLeveling(shutdownSend.ch, shutdownReceive)
		close(shutdownReceive)
	}()
	return shutdownSend, shutdownReceive
}

func splitStopper(shutdownLevels <-chan ShutdownDisposition) (_, _, _ <-chan ShutdownDisposition) {
	var lsnShutdownSignals,
		srvShutdownSignals,
		mntShutdownSignals <-chan ShutdownDisposition
	relayUnordered(shutdownLevels, &lsnShutdownSignals,
		&srvShutdownSignals, &mntShutdownSignals)
	return lsnShutdownSignals, srvShutdownSignals, mntShutdownSignals
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
				stopper.send(ShutdownPatient)
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

func setupExtraStopWriters(
	idleCheck time.Duration, fsys *fileSystem,
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

func logListeners(log ulog.Logger, listeners <-chan manet.Listener) {
	for l := range listeners {
		log.Printf("listening on: %s\n", l.Multiaddr())
	}
}

func sequentialLeveling(stopper <-chan ShutdownDisposition, filtered chan<- ShutdownDisposition) {
	var highestSeen ShutdownDisposition
	for level := range stopper {
		if level > highestSeen {
			highestSeen = level
			filtered <- level
		}
	}
}

func watchListenersStopper(cancel context.CancelFunc,
	stopper <-chan ShutdownDisposition, log ulog.Logger,
) {
	for range stopper {
		log.Print("stop signal received - not accepting new listeners")
		cancel()
		return
	}
}

func unmountAll(system mountSubsystem,
	levels <-chan ShutdownDisposition,
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

func stopOnDone(ctx context.Context, shutdownSend wgShutdown) {
	defer shutdownSend.Done()
	select {
	case <-ctx.Done():
		shutdownSend.send(ShutdownImmediate)
	case <-shutdownSend.closing:
	}
}

func stopOnUnreachable(fsys *fileSystem, stopper wgShutdown,
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
		unreachableCheckFn = func() (bool, ShutdownDisposition, error) {
			shutdown, _, err := idleCheckFn()
			if !shutdown || err != nil {
				return keepRunning, dontShutdown, err
			}
			haveNetwork, err := hasEntries(listeners)
			if haveNetwork || err != nil {
				return keepRunning, dontShutdown, err
			}
			log.Print(idleMessage)
			return stopRunning, ShutdownImmediate, nil
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
			log.Printf(`external source requested to shutdown: "%s"`, level.String())
			if !stopper.send(level) {
				return
			}
		case <-stopper.Closing():
			return
		}
	}
}

func parseDispositionData(data []byte) (ShutdownDisposition, error) {
	if len(data) != 1 {
		str := strings.TrimSpace(string(data))
		return generic.ParseEnum(minimumShutdown, maximumShutdown, str)
	}
	level := ShutdownDisposition(data[0])
	if level < minimumShutdown || level > maximumShutdown {
		return 0, fmt.Errorf("%w:"+
			"got: %d, valid level range is: %d:%d",
			errShutdownDisposition, level,
			minimumShutdown, maximumShutdown,
		)
	}
	return level, nil
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
func makeIdleChecker(fsys *fileSystem, interval time.Duration, log ulog.Logger) checkFunc {
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
	return func() (bool, ShutdownDisposition, error) {
		mounted, err := hasEntries(mounts)
		if mounted || err != nil {
			return keepRunning, dontShutdown, err
		}
		activeConns, err := hasActiveClients(listeners, interval)
		if activeConns || err != nil {
			return keepRunning, dontShutdown, err
		}
		log.Print(idleMessage)
		return stopRunning, ShutdownImmediate, nil
	}
}
