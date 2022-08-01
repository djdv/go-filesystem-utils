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

	"github.com/djdv/go-filesystem-utils/internal/files"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/u-root/uio/ulog"
)

type (
	Server struct {
		root   *files.Directory
		server *p9.Server

		closer io.Closer
		log    ulog.Logger

		servingWg sync.WaitGroup
		uid       p9.UID
		gid       p9.GID
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
		uid: p9.NoUID,
		gid: p9.NoGID,
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
	)
	if root == nil {
		const (
			owner       = 6
			group       = 3
			permissions = p9.Exec<<owner | p9.Write<<owner | p9.Read<<owner |
				p9.Exec<<group | p9.Read<<group |
				p9.Exec | p9.Read
		)
		root = files.NewDirectory(
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
	var (
		closed bool

		sockName = strings.ReplaceAll(
			listener.Addr().String(),
			"/", "|",
		)
		listenerCloser closer = func() error {
			closed = true
			return listener.Close()
		}
	)
	if err := addCloserFile(sockName, listenerCloser, root); err != nil {
		return err
	}

	var (
		wg                sync.WaitGroup
		conns, acceptErrs = accept(ctx, &closed, listener)
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

// [45ecbfb2-430b-48e0-847d-a6f78eac7816]
// TODO: root type should be p9.File
// path should be constructed and passed to root, and shared with newCloser
// not retrieved from getter method.
func addCloserFile(name string, closer io.Closer, root *files.Directory) error {
	listenerDir, err := getListenerDir(root)
	if err != nil {
		return err
	}
	closerFile, _ := files.NewCloser(closer,
		files.WithParent[files.CloserOption](listenerDir),
		files.WithPath[files.CloserOption](root.Path()),
	)
	if err := listenerDir.Link(closerFile, name); err != nil {
		return err
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
		owner = 6
		group = 3
		// TODO: permission audit
		// Do we want to allow non-owner to list these? exec may be fine, but not read.
		permissions = p9.Exec<<owner | p9.Write<<owner | p9.Read<<owner |
			p9.Exec<<group | p9.Read<<group |
			p9.Exec | p9.Read
	)
	if _, err := root.Mkdir(dirName, permissions, attr.UID, attr.GID); err != nil {
		return nil, err
	}
	_, listenerDir, err = root.Walk(wnames)
	return listenerDir, err
}

func accept(ctx context.Context, closed *bool, listener manet.Listener) (<-chan manet.Conn, <-chan error) {
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
				if *closed && // Listener closed intentionally.
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
