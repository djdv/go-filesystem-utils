package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
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
		srvLog      ulog.Logger
		nineOpts    []p9.ServerOpt
	)
	if lazy, ok := serverMaddr.(lazyFlag[multiaddr.Multiaddr]); ok {
		serverMaddr = lazy.get()
	}
	if set.verbose {
		srvLog = log.New(os.Stdout, "⬆️ server - ", log.Lshortfile)
		nineOpts = []p9.ServerOpt{p9.WithServerLogger(srvLog)}
	} else {
		srvLog = ulog.Null
	}
	//
	var (
		sigCtx, sigCancel = context.WithCancel(ctx)
		interruptCount    = signalCount(sigCtx, os.Interrupt)

		serversWg sync.WaitGroup
		server    *p9.Server

		serveErrs = make(chan error)

		netMan      = new(listenerManager)
		netCallback = func(listener manet.Listener) {
			serversWg.Add(1)
			go func() {
				defer serversWg.Done()
				if err := serve(ctx, server, netMan, listener); err != nil {
					select {
					case serveErrs <- err:
					case <-ctx.Done():
					}
				}
			}()
		}
		fsys, netsys  = newFileSystem(set.uid, set.gid, netCallback)
		listener, err = netsys.Listen(serverMaddr)
	)
	defer sigCancel()
	if err != nil {
		return err
	}
	server = p9.NewServer(fsys, nineOpts...)
	netCallback(listener)

	// TODO: share same shutdown function between interrupt, idle, et al. server-watchers

	interruptErrs := shutdownOnInterrupt(sigCtx, sigCancel, interruptCount, netMan)
	go func() {
		defer sigCancel()
		defer close(serveErrs)
		serversWg.Wait()
	}()

	srvLog.Print("listening on: ", serverMaddr)

	errs := []<-chan error{serveErrs, interruptErrs}
	if isPipe(os.Stdin) {
		errs = append(errs,
			handleStdio(sigCtx, server))
	}
	if interval := set.exitInterval; interval != 0 {
		errs = append(errs,
			shutdownOnIdle(ctx, interval, fsys, netMan))
	}

	// TODO: we need to act on all errors as they come in
	// not just all of them at time of exit.
	// (Trigger shutdown on first encountered?)
	if err := flattenErrs(errs...); err != nil {
		if !errors.Is(err, net.ErrClosed) {
			return err
		}
	}
	return nil
}

func newFileSystem(uid p9.UID, gid p9.GID, netCallback files.ListenerCallback) (*files.Directory, *files.Listener) {
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
		path        = new(atomic.Uint64)
		options     = []files.MetaOption{files.WithPath(path)}
		_, fsys     = files.NewDirectory(options...)
		listenerDir = files.NewListener(netCallback, options...)
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
			File: listenerDir,
		},
	} {
		if err := file.SetAttr(valid, attr); err != nil {
			panic(err)
		}
		if err := fsys.Link(file.File, file.name); err != nil {
			panic(err)
		}
	}
	return fsys, listenerDir
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
		sendErr := func(err error) {
			select {
			case errs <- err:
			case <-ctx.Done():
			}
		}
		if err := server.Handle(os.Stdin, os.Stdout); err != nil {
			if !errors.Is(err, io.EOF) {
				sendErr(err)
				return
			}
		}
		if err := os.Stderr.Close(); err != nil {
			sendErr(err)
			return
		}
		if err := reopenNullStdio(); err != nil {
			sendErr(err)
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

func shutdownOnInterrupt(ctx context.Context, cancel context.CancelFunc,
	counter <-chan uint, netMan *listenerManager,
) <-chan error {
	var (
		errs      = make(chan error)
		sawSignal bool
	)
	go func() {
		defer cancel()
		var connectionsCancel context.CancelFunc
		for {
			select {
			case signalCount := <-counter:
				switch signalCount {
				case 1: // "Close".
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
						// FIXME: Do this elsewhere.
						// close(server.mknodServerErrs)
					}()
				case 2: // "Close now".
					// TODO: Notify clients?:
					// mknod `/listeners/shuttingdown` {$time.Time}
					go func() {
						<-time.After(10 * time.Second)
						connectionsCancel()
					}()
				case 3: // "Close right now".
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
		sendErr := func(err error) {
			select {
			case errs <- err:
			case <-ctx.Done():
			}
		}
		_, mounterDir, err := fsys.Walk([]string{mounterName})
		if err != nil {
			sendErr(err)
			return
		}
		idleCheckTicker := time.NewTicker(interval)
		defer idleCheckTicker.Stop()
		for {
			select {
			case <-idleCheckTicker.C:
				log.Println("checking if busy...")
				busy, err := haveMounts(mounterDir)
				if err != nil {
					log.Println("err:", err)
					sendErr(err)
					return
				}
				if busy {
					log.Println("we're busy.")
					continue
				}
				log.Println("we're not busy - shutting down")
				if err := shutdown(ctx, netMan); err != nil {
					log.Println("shutdown err:", err)
					sendErr(err)
				}
				log.Println("shutdown")
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
