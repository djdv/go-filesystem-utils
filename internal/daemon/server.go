package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/djdv/go-filesystem-utils/internal/files"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/u-root/uio/ulog"
)

type (
	Server struct {
		path   *atomic.Uint64
		root   *files.Directory
		server *p9.Server

		closer io.Closer
		log    ulog.Logger

		uid p9.UID
		gid p9.GID
		// stopping  bool

		// experimenting:
		// activeConnsWg []*sync.WaitGroup
		// shutdown <-chan struct{}
	}

	closer func() error

	handleFunc func(t io.ReadCloser, r io.WriteCloser) error
)

func (fn closer) Close() error { return fn() }

func NewServer(options ...ServerOption) *Server {
	server := &Server{
		path: new(atomic.Uint64),
		uid:  p9.NoUID,
		gid:  p9.NoGID,
	}
	for _, setFunc := range options {
		if err := setFunc(server); err != nil {
			panic(err)
		}
	}
	return server
}

func (srv *Server) ListenAndServe(ctx context.Context, maddr multiaddr.Multiaddr) error {
	listener, err := manet.Listen(maddr)
	if err != nil {
		return err
	}

	// TODO: lint; for testing  multiple listener logic
	// Needs to be wired up to CLI somehow, and/or converted for test pkg.
	// something like `daemon addServer existingDaemonSock newDaemonMaddr`
	// (the likelyhood this will be needed is slim)
	/*
		go func() {
				const maddrString = "/ip4/127.0.0.1/tcp/564"
				listener, _ := manet.Listen(multiaddr.StringCast(maddrString))
				srv.Serve(ctx, listener)
			}()
			go func() {
				const maddrString = "/ip4/127.0.0.1/tcp/565"
				listener, _ := manet.Listen(multiaddr.StringCast(maddrString))
				srv.Serve(ctx, listener)
			}()
	*/

	return srv.Serve(ctx, listener)
}

// TODO: create a file in the server when shutdown is triggered
// its contents should be the timeout message, something like
// `shutdown triggered, active connections will be closed at $time`
// within `/dying` or something. Maybe a less morbid name.
func (srv *Server) Serve(ctx context.Context, listener manet.Listener) error {
	// TODO: close on cancel?
	var (
		root   = srv.root
		server = srv.server
		path   = srv.path
	)
	if root == nil {
		const permissions = S_IRWXU |
			S_IRGRP | S_IXGRP |
			S_IRUSR | S_IXUSR
		root = files.NewDirectory(
			files.WithPath[files.DirectoryOption](path),
			files.WithPermissions[files.DirectoryOption](permissions),
			files.WithUID[files.DirectoryOption](srv.uid),
			files.WithGID[files.DirectoryOption](srv.gid),
		)
		var serverOpts []p9.ServerOpt
		if srvLog := srv.log; srvLog != nil {
			serverOpts = append(serverOpts, p9.WithServerLogger(srvLog))
		}
		server = p9.NewServer(root, serverOpts...)
		srv.root = root
		srv.server = server
	}

	closed, err := storeListener(listener, root, path, srv.uid, srv.gid)
	if err != nil {
		return err
	}

	var (
		wg                sync.WaitGroup
		conns, acceptErrs = accept(ctx, closed, listener)
		handlerErrs       = handle9(ctx, conns, server.Handle, &wg)

		// TODO: gross
		// also, we should just log (to null if !verbose) errors
		// maybe store the first or last, but not all of them
		// sentinal value would be fine; ErrProblemLight("check your logs, not my problem")
		// otherwise we could balloon hard if someone spams the server. (oom via discon atk)
		retErr    error
		appendErr = func(err error) {
			// log.Printf("saw err in Serve: %v", err)
			if retErr == nil {
				// TODO: wrap with some prefix
				retErr = err
			} else {
				// TODO: better format?
				retErr = fmt.Errorf("%w\n\t%s", retErr, err)
			}
		}
	)

	for acceptErrs != nil ||
		handlerErrs != nil {
		select {
		case err, ok := <-acceptErrs:
			if !ok {
				acceptErrs = nil
				continue
			}
			log.Println("hit 1:", err)
			appendErr(err)
		case err, ok := <-handlerErrs:
			if !ok {
				handlerErrs = nil
				continue
			}
			log.Println("hit 2:", err)
			appendErr(err)
		}
	}

	wg.Wait()
	return retErr
}

// TODO: docs
// bool will be true when some arbitrary thread calls close.
// We need this to distinguish between an intentional close
// and unexpected socket failure.
func storeListener(listener manet.Listener, root p9.File,
	path *atomic.Uint64, uid p9.UID, gid p9.GID,
) (*atomic.Bool, error) {
	listenersDir, err := getListenerDir(root)
	if err != nil {
		return nil, err
	}
	return storeListenerFile(listener, listenersDir, path, uid, gid)
}

func storeListenerFile(listener manet.Listener, listenersDir p9.File,
	path *atomic.Uint64, uid p9.UID, gid p9.GID,
) (*atomic.Bool, error) {
	var (
		// TODO: [safety] The prefix trim should be safe by the spec
		// I'm not sure if we have guarantees about component counts.
		components = strings.Split(listener.Multiaddr().String(), "/")[1:]
		tail       = len(components) - 1
		dirs       = components[:tail]
		file       = components[tail]
	)
	listenerDir, err := mkdirAll(listenersDir, dirs, uid, gid)
	if err != nil {
		return nil, err
	}
	var (
		closerFile p9.File
		closed     = new(atomic.Bool)
		closeFunc  = closer(func() error {
			closed.Store(true)
			lErr := listener.Close()

			// TODO: handle non-listeners errors
			if err := closerFile.UnlinkAt(".", 0); err != nil {
				log.Printf("DBG: unlink %s err: %s", file, err)
			}
			if err := removeEmpties(listenersDir, dirs); err != nil {
				log.Printf("DBG: cleanup for %s - %s err: %s", dirs, file, err)
			}

			return lErr
		})
	)
	closerFile = makeListenerFile(closeFunc, listenerDir, path, uid, gid)
	if err := listenerDir.Link(closerFile, file); err != nil {
		return nil, err
	}
	return closed, nil
}

func makeListenerFile(closer io.Closer, parent p9.File,
	path *atomic.Uint64, uid p9.UID, gid p9.GID,
) p9.File {
	closerFile, _ := files.NewCloser(closer,
		files.WithParent[files.CloserOption](parent),
		files.WithPath[files.CloserOption](path),
		files.WithUID[files.CloserOption](uid),
		files.WithGID[files.CloserOption](gid),
	)
	return closerFile
}

func mkdirAll(dir p9.File, names []string, uid p9.UID, gid p9.GID) (p9.File, error) {
	var (
		closers  = make([]io.Closer, 0, len(names))
		closeAll = func() error {
			for _, c := range closers {
				if err := c.Close(); err != nil {
					return err
				}
			}
			closers = nil
			return nil
		}
	)
	defer closeAll() // TODO: error needs to be caught and appended if we return early.

	const (
		// TODO: permission audit
		permissions = S_IRWXU |
			S_IXGRP | S_IRGRP |
			S_IXOTH | S_IROTH
	)
	tail := len(names) - 1
	for i, name := range names {
		name9 := []string{name}
		_, next, err := dir.Walk(name9)
		if err != nil {
			if !errors.Is(err, perrors.ENOENT) {
				return nil, err
			}
			if _, err := dir.Mkdir(name, permissions, uid, gid); err != nil {
				return nil, err
			}
			if _, next, err = dir.Walk(name9); err != nil {
				return nil, err
			}
		}
		if i != tail {
			closers = append(closers, next)
		}
		dir = next
	}
	if err := closeAll(); err != nil {
		return nil, err
	}
	return dir, nil
}

// XXX this whole thang is likely more nasty than it has to be.
// If anything fails in here we're likely going to get zombie files that might ruin things.
// Likely fine for empty directories, but not endpoints. That shouldn't happen though.
// "shouldn't"
// TODO: name needs to imply reverse order, or take an order param
func removeEmpties(root p9.File, dirs []string) error {
	var (
		cur      = root
		nwname   = len(dirs)
		dirFiles = make([]p9.File, nwname)

		// TODO: micro-opt; is this faster than allocating in the loop?
		wname = make([]string, 1)
	)
	for i, name := range dirs {
		wname[0] = name
		_, dir, err := cur.Walk(wname)
		if err != nil {
			return err
		}
		cur = dir
		dirFiles[i] = cur
	}
	for i := nwname - 1; i >= 0; i-- {
		cur := dirFiles[i]
		ents, err := files.ReadDir(cur)
		if err != nil {
			return err
		}
		if len(ents) == 0 {
			// XXX: we're avoiding `Walk(..)` here
			// but it's hacky and gross. Our indexing should be better,
			// or we should just do the walk.
			var (
				parent p9.File
				name   = dirs[i]
			)
			if i == 0 {
				parent = root
			} else {
				parent = dirFiles[i-1]
			}
			if err := parent.UnlinkAt(name, 0); err != nil {
				return err
			}
		}
	}
	return nil
}

func getListenerDir(root p9.File) (p9.File, error) {
	const (
		dirName = "listeners"
		flags   = p9.WriteOnly
	)
	wnames := []string{dirName}
	_, listenerDir, err := root.Walk(wnames)
	if err == nil {
		return listenerDir, nil
	}
	if !errors.Is(err, perrors.ENOENT) {
		return nil, err
	}

	want := p9.AttrMask{
		// Mode: true,
		UID: true,
		GID: true,
	}
	_, filled, attr, err := root.GetAttr(want)
	if err != nil {
		return nil, err
	}

	if !filled.Contains(want) {
		// TODO: better errmsg
		return nil, fmt.Errorf("couldn't get required stat from root")
	}

	const (
		// TODO: permission audit
		// Do we want to allow non-owner to list these? exec may be fine, but not read.
		permissions = S_IRWXU |
			S_IXGRP | S_IRGRP |
			S_IXOTH | S_IROTH
	)
	if _, err := root.Mkdir(dirName, permissions, attr.UID, attr.GID); err != nil {
		return nil, err
	}
	_, listenerDir, err = root.Walk(wnames)
	return listenerDir, err
}

func accept(ctx context.Context, closed *atomic.Bool, listener manet.Listener) (<-chan manet.Conn, <-chan error) {
	var (
		conns = make(chan manet.Conn)
		errs  = make(chan error)
	)
	go func() {
		defer close(conns)
		defer close(errs)
		for {
			conn, err := listener.Accept()
			if err != nil {
				if closed.Load() && // Listener closed intentionally.
					errors.Is(err, net.ErrClosed) {
					return
				}
				select {
				case errs <- err:
				case <-ctx.Done():
				}
				return
			}
			select {
			case conns <- conn:
			case <-ctx.Done():
				listener.Close()
				return
			}
		}
	}()
	return conns, errs
}

func handle9(ctx context.Context,
	conns <-chan manet.Conn, handle handleFunc, wg *sync.WaitGroup,
) <-chan error {
	errs := make(chan error)
	go func() {
		defer func() {
			wg.Wait()
			close(errs)
		}()
		for conn := range conns {
			wg.Add(1)
			go func(cn manet.Conn) {
				/* TODO: lint
				 p9.Handle closes t+r internally when it returns
				 we do NOT get their errors
				defer func() {
					if err := cn.Close(); err != nil {
						fmt.Println("handle9 hit 1:", err)
						select {
						case errs <- err:
						case <-ctx.Done():
						}
					}
					wg.Done()
				}()
				*/
				defer wg.Done()

				// TODO: we need some way to track active connections in the caller
				// or close on a signal within here.
				// ^ So that when the server is told to stop, we don't wait
				// indefinitely for clients to close first.
				// We can give them some grace period, but not forever (as is currently the case).

				if err := handle(cn, cn); err != nil {
					if !errors.Is(err, io.EOF) {
						select {
						case <-ctx.Done():
						case errs <- err:
						}
					}
				}
			}(conn)
		}
	}()
	return errs
}
