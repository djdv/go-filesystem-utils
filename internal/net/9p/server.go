package p9

import (
	"context"
	"errors"
	"io"
	gonet "net"
	"sync"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/net"
	"github.com/hugelgupf/p9/p9"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	Server struct {
		*net.ListenerManager
		*p9.Server
	}
	serverHandleFunc = func(io.ReadCloser, io.WriteCloser) error
)

func NewServer(attacher p9.Attacher, options ...p9.ServerOpt) *Server {
	return &Server{
		ListenerManager: new(net.ListenerManager),
		Server:          p9.NewServer(attacher, options...),
	}
}

func (srv *Server) Serve(ctx context.Context,
	listener manet.Listener,
) <-chan error {
	var (
		listMan      = srv.ListenerManager
		connMan, err = listMan.Add(listener)
		errs         = make(chan error)
		maybeSendErr = func(err error) {
			select {
			case errs <- err:
			case <-ctx.Done():
			}
		}
	)
	go func() {
		defer close(errs)
		if err != nil {
			maybeSendErr(err)
			return
		}
		defer listMan.Remove(listener)
		var (
			connectionsWg     sync.WaitGroup
			acceptCtx, cancel = context.WithCancel(ctx)
			conns, acceptErrs = accept(acceptCtx, listener)
			handleMessages    = srv.Handle
		)
		defer cancel()
		for connOrErr := range generic.CtxEither(acceptCtx, conns, acceptErrs) {
			var (
				conn = connOrErr.Left
				err  = connOrErr.Right
			)
			if err != nil {
				select {
				case errs <- err:
					continue
				case <-ctx.Done():
					return
				}
			}
			connectionsWg.Add(1)
			go func(cn manet.Conn) {
				defer connectionsWg.Done()
				defer connMan.Remove(cn)
				tc, err := connMan.Add(cn)
				if err != nil {
					maybeSendErr(err)
					return
				}
				if err := handleMessages(tc, tc); err != nil {
					if !errors.Is(err, io.EOF) {
						maybeSendErr(err)
					}
				}
			}(conn)
		}
		connectionsWg.Wait()
	}()
	return errs
}

func accept(ctx context.Context, listener manet.Listener) (<-chan manet.Conn, <-chan error) {
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
				if !errors.Is(err, gonet.ErrClosed) {
					select {
					case errs <- err:
					case <-ctx.Done():
					}
				}
				return
			}
			select {
			case conns <- conn:
			case <-ctx.Done():
				conn.Close()
				return
			}
		}
	}()
	return conns, errs
}

/*
func (srv *Server) Shutdown(ctx context.Context) error {
	if err := srv.ListenerManager.Shutdown(ctx); err != nil {
		return err
	}
	return nil
}
*/
