package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/command"
	"github.com/djdv/go-filesystem-utils/internal/daemon"
	"github.com/djdv/go-filesystem-utils/internal/files"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/u-root/uio/ulog"
)

type (
	daemonSettings struct {
		serverMaddr  multiaddr.Multiaddr
		exitInterval time.Duration
		uid          p9.UID
		gid          p9.GID
		commonSettings
	}
)

func (set *daemonSettings) BindFlags(flagSet *flag.FlagSet) {
	set.commonSettings.BindFlags(flagSet)
	multiaddrVar(flagSet, &set.serverMaddr, daemon.ServerName,
		defaultServerMaddr{}, "Listening socket `maddr`.")
	flagSet.DurationVar(&set.exitInterval, "exit-after",
		0, "Check every `interval` (e.g. \"30s\") if the service is active and exit if not.")
	// TODO: default should be current user ids on unix, NoUID on NT.
	uidVar(flagSet, &set.uid, "uid",
		p9.NoUID, "file owner's `uid`")
	gidVar(flagSet, &set.gid, "gid",
		p9.NoGID, "file owner's `gid`")
}

func Daemon() command.Command {
	const (
		name     = "daemon"
		synopsis = "Hosts the service."
		usage    = "Placeholder text."
	)
	return command.MakeCommand[*daemonSettings](name, synopsis, usage, daemonExecute)
}

const (
	// TODO: These should be exported for clients.
	// But need good names and docs.
	// And docs that link "X is the [Y] fs" where Y links to docs
	// for the Go and 9P interfaces of that FS.

	listenerName = "listeners"
	mounterName  = "mounts"
)

func daemonExecute(ctx context.Context, set *daemonSettings) error {
	var ( // TODO: [31f421d5-cb4c-464e-9d0f-41963d0956d1]
		serverMaddr = set.serverMaddr
		srvLog      = makeDaemonLog(set.verbose)
	)
	if lazy, ok := serverMaddr.(lazyFlag[multiaddr.Multiaddr]); ok {
		serverMaddr = lazy.get()
	}
	var (
		listenersWg    sync.WaitGroup
		handler        serverHandleFunc
		listMan        = new(listenerManager)
		serveErrs      = make(chan error)
		handleListener = func(listener manet.Listener) {
			listenersWg.Add(1)
			srvLog.Print("listening on: ", listener.Multiaddr())
			go func() {
				defer listenersWg.Done()
				for err := range serve(ctx, listener, listMan, handler) {
					select {
					case serveErrs <- err:
					case <-ctx.Done():
						return
					}
				}
				srvLog.Print("done listening on: ", listener.Multiaddr())
			}()
		}
		fsys, netsys                     = newSystems(set.uid, set.gid, handleListener)
		server                           = p9.NewServer(fsys, p9.WithServerLogger(srvLog))
		sigCtx, sigCancel, interruptErrs = shutdownOnInterrupt(ctx, listMan)
		listener, err                    = netsys.Listen(serverMaddr)
		errs                             = []<-chan error{serveErrs, interruptErrs}
	)
	if err != nil {
		sigCancel()
		return err
	}
	handler = server.Handle
	handleListener(listener)
	go func() { defer sigCancel(); defer close(serveErrs); listenersWg.Wait() }()
	if isPipe(os.Stdin) {
		errs = append(errs, handleStdio(sigCtx, server))
	}
	if interval := set.exitInterval; interval != 0 {
		errs = append(errs, shutdownOnIdle(ctx, interval, fsys, listMan))
	}
	return flattenErrs(errs...)
}

func makeDaemonLog(verbose bool) ulog.Logger {
	if verbose {
		return log.New(os.Stdout, "⬆️ server - ", log.Lshortfile)
	}
	return ulog.Null
}

func shutdownOnInterrupt(ctx context.Context, listMan *listenerManager) (context.Context, context.CancelFunc, <-chan error) {
	var (
		sigCtx, sigCancel = context.WithCancel(ctx)
		interruptCount    = signalCount(sigCtx, os.Interrupt)
	)
	return sigCtx, sigCancel, shutdownWithCounter(sigCtx, sigCancel, interruptCount, listMan)
}

func signalCount(ctx context.Context, sig os.Signal) <-chan uint {
	var (
		counter = make(chan uint)
		signals = make(chan os.Signal, 1)
	)
	signal.Notify(signals, sig)
	go func() {
		defer close(counter)
		defer close(signals)
		defer signal.Ignore(sig)
		var count uint
		for {
			select {
			case <-signals:
				count++
				select {
				case counter <- count:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return counter
}

func newSystems(uid p9.UID, gid p9.GID, netCallback files.ListenerCallback) (*files.Directory, *files.Listener) {
	const permissions = files.S_IRWXU |
		files.S_IRGRP | files.S_IXGRP |
		files.S_IROTH | files.S_IXOTH
	var (
		valid = p9.SetAttrMask{
			Permissions: true,
			UID:         true,
			GID:         true,
			ATime:       true,
			MTime:       true,
			CTime:       true,
		}
		attr = p9.SetAttr{
			Permissions: permissions,
			UID:         uid,
			GID:         gid,
		}
		options = []files.MetaOption{
			files.WithPath(new(atomic.Uint64)),
		}
		fsys      = newFileSystem(options...)
		listeners = newNetwork(netCallback, options...)
	)
	if err := fsys.SetAttr(valid, attr); err != nil {
		panic(err)
	}
	for _, file := range []struct {
		p9.File
		name string
	}{
		{
			name: mounterName,
			File: files.NewMounter(options...),
		},
		{
			name: listenerName,
			File: listeners,
		},
	} {
		if err := file.SetAttr(valid, attr); err != nil {
			panic(err)
		}
		if err := fsys.Link(file.File, file.name); err != nil {
			panic(err)
		}
	}
	return fsys, listeners
}

func newFileSystem(options ...files.MetaOption) *files.Directory {
	_, fsys := files.NewDirectory(options...)
	return fsys
}

func newNetwork(handleListener files.ListenerCallback, options ...files.MetaOption) *files.Listener {
	_, listenerDir := files.NewListener(handleListener, options...)
	return listenerDir
}

func isPipe(file *os.File) bool {
	fStat, err := file.Stat()
	if err != nil {
		return false
	}
	return fStat.Mode().Type()&os.ModeNamedPipe != 0
}

func handleStdio(ctx context.Context, server *p9.Server) <-chan error {
	errs := make(chan error)
	go func() {
		defer close(errs)
		if err := server.Handle(os.Stdin, os.Stdout); err != nil {
			if !errors.Is(err, io.EOF) {
				maybeSendErr(ctx, errs, err)
				return
			}
		}
		if err := os.Stderr.Close(); err != nil {
			maybeSendErr(ctx, errs, err)
			return
		}
		if err := reopenNullStdio(); err != nil {
			maybeSendErr(ctx, errs, err)
		}
	}()
	return errs
}

func reopenNullStdio() error {
	const stdioMode = 0o600
	discard, err := os.OpenFile(os.DevNull, os.O_RDWR, stdioMode)
	if err != nil {
		return err
	}
	for _, f := range []**os.File{&os.Stdin, &os.Stdout, &os.Stderr} {
		*f = discard
	}
	return nil
}

// NOTE: This is a server, which serves a 9P directory,
// containing multiaddr files that match the listening addresses passed in.
// (Typically used between the daemon and
// a client that spawned the daemon's process - via stdio.)
/* DEPRECATED; actual server's listener dir is passed through flatfunc now instead.
func newListenerServer(listeners ...manet.Listener) (*p9.Server, error) {
	root, err := newListenerFlatDir(listeners...)
	if err != nil {
		return nil, err
	}
	return p9.NewServer(root), nil
}

func newListenerFlatDir(listeners ...manet.Listener) (*files.Directory, error) {
	var (
		_, root         = files.NewDirectory()
		_, listenersDir = files.NewDirectory(files.WithParent(root))
	)
	// TODO: name const
	if err := root.Link(listenersDir, "listeners"); err != nil {
		return nil, err
	}
	for i, listener := range listeners {
		listenerFile := staticfs.ReadOnlyFile(
			listener.Multiaddr().String(),
			p9.QID{Type: p9.TypeRegular},
		)
		if err := listenersDir.Link(listenerFile, strconv.Itoa(i)); err != nil {
			return nil, err
		}
	}
	return root, nil
}
*/

func flattenErrs(errs ...<-chan error) (err error) {
	for e := range generic.CtxMerge(context.Background(), errs...) {
		if err == nil {
			err = e
		} else {
			err = fmt.Errorf("%w\n%s", err, e)
		}
	}
	return
}

func joinErrs(errs ...error) (err error) {
	for _, e := range errs {
		if err == nil {
			err = e
		} else {
			err = fmt.Errorf("%w\n%s", err, e)
		}
	}
	return
}

func shutdownWithCounter(ctx context.Context, cancel context.CancelFunc,
	counter <-chan uint, netMan *listenerManager,
) <-chan error {
	var (
		errs      = make(chan error)
		sawSignal bool
	)
	go func() {
		defer cancel()
		const (
			waitForConns = 1
			timeoutConns = 2
			closeConns   = 3
		)
		var connectionsCancel context.CancelFunc
		for {
			select {
			case signalCount := <-counter:
				switch signalCount {
				case waitForConns:
					var connectionsCtx context.Context
					sawSignal = true
					connectionsCtx, connectionsCancel = context.WithCancel(ctx)
					go func() {
						defer close(errs)
						defer connectionsCancel()
						if err := shutdown(connectionsCtx, netMan); err != nil {
							select {
							case errs <- err:
							case <-ctx.Done():
							}
						}
					}()
				case timeoutConns:
					// TODO: Notify clients?:
					// mknod `/listeners/shuttingdown` {$time.Time}
					go func() {
						// TODO: const
						<-time.After(10 * time.Second)
						connectionsCancel()
					}()
				case closeConns:
					connectionsCancel()
					return
				}
			case <-ctx.Done():
				if !sawSignal {
					close(errs)
				}
				return
			}
		}
	}()
	return errs
}

func shutdownOnIdle(ctx context.Context, interval time.Duration,
	fsys p9.File, netMan *listenerManager,
) <-chan error {
	errs := make(chan error)
	go func() {
		defer close(errs)
		_, mounterDir, err := fsys.Walk([]string{mounterName})
		if err != nil {
			maybeSendErr(ctx, errs, err)
			return
		}
		idleCheckTicker := time.NewTicker(interval)
		defer idleCheckTicker.Stop()
		for {
			select {
			case <-idleCheckTicker.C:
				busy, err := haveMounts(mounterDir)
				if err != nil {
					maybeSendErr(ctx, errs, err)
					return
				}
				if busy {
					continue
				}
				if err := shutdown(ctx, netMan); err != nil {
					maybeSendErr(ctx, errs, err)
				}
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return errs
}

func haveMounts(mounterDir p9.File) (bool, error) {
	ents, err := files.ReadDir(mounterDir)
	if err != nil {
		return false, err
	}
	return len(ents) > 0, nil
}

func maybeSendErr(ctx context.Context, errs chan<- error, err error) {
	select {
	case errs <- err:
	case <-ctx.Done():
	}
}
