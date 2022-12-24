package p9

import (
	"sync/atomic"

	srv9 "github.com/djdv/go-filesystem-utils/internal/net/9p"
	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
)

type (
	NineDir struct {
		p9.File
		path *atomic.Uint64
	}
	nineInterfaceFile struct {
		templatefs.NoopFile
		// nineServer *p9.Server // TODO: use our shutdown-able server implementation instead.
		nineServer *srv9.Server // TODO: use our shutdown-able server implementation instead.
		metadata
	}
)

func NewNineDir(options ...NineOption) (p9.QID, *NineDir) {
	panic("NIY")
	/*
	   var (

	   	server = &srv9.Server{
	   		// TODO: is a proper srv9.New func possible with our callbacks?
	   		ListenerManager: new(net.ListenerManager),
	   	}
	   	handleListener = func(listener manet.Listener) {
	   		go func() {
	   			defer listenersWg.Done()
	   			for err := range server.Serve(ctx, listener) {
	   				select {
	   				case serveErrs <- err:
	   				case <-ctx.Done():
	   					return
	   				}
	   			}
	   			srvLog.Print("done listening on: ", listener.Multiaddr())
	   		}()
	   	}

	   	qid, dir = NewListener(handleListener, options...)
	   	fsys     = &NineDir{File: dir, path: dir.path}

	   )
	   // TODO: funcopt needs RDev setter? SetAttr doesn't expose it,
	   // and neither do some of our types.
	   // dir.RDev = p9.Dev(filesystem.Plan9Protocol)
	   return qid, fsys
	*/
}

/* Code circa 2017 - deprecated by netsys/listener
func listen9(ctx context.Context, maddr string, server *p9.Server) (serverRef, error) {
	// parse and listen on the address
	ma, err := multiaddr.NewMultiaddr(maddr)
	if err != nil {
		return serverRef{}, err
	}

	mListener, err := manet.Listen(ma)
	if err != nil {
		return serverRef{}, err
	}

	// construct the actual reference
	// NOTE: [async]
	// `srvErr` will be set only once
	// The `err` function checks a "was set" boolean to assure the `error` is fully assigned, before trying to return it
	// This is because `ref.err` will be called without synchronization, and could cause a read/write collision on an `error` type
	// We don't have to care about a bool's value being fully written or not, but a partially written `error` is an node with an arbitrary value
	// `decRef` has synchronization, so it may use `srvErr` directly (after syncing)
	// The counter however, will only ever be manipulated while the reference table is in a locked state
	// (if this changes, the counter should be made atomic)
	var (
		srvErr       error
		srvErrWasSet bool
		srvWg        sync.WaitGroup // done when the server has stopped serving
		count        uint
	)

	serverRef := serverRef{
		Listener: mListener,
		incRef:   func() { count++ },
		err: func() error {
			if srvErrWasSet {
				return srvErr
			}
			return nil
		},
		decRef: func() error {
			count--
			if count == 0 {
				lstErr := mListener.Close() // will trigger the server to stop
				srvWg.Wait()                // wait for the server to assign its error

				if srvErr == nil && lstErr != nil { // server didn't encounter an error, but the listener did
					return lstErr
				}

				err := srvErr      // server encountered an error
				if lstErr != nil { // append the listener error if it encountered one too
					err = fmt.Errorf("%w; additionally the listener encountered an error on `Close`: %s", err, lstErr)
				}

				return err
			}
			return nil
		},
	}

	// launch the  resource server instance in the background
	// until either an error is encountered, or the listener is closed (which forces an "accept" error)
	srvWg.Add(1)
	go func() {
		defer srvWg.Done()
		if err := server.Serve(manet.NetListener(mListener)); err != nil {
			if ctx.Err() != nil {
				var opErr *gonet.OpError
				if errors.As(err, &opErr) && opErr.Op != "accept" {
					err = nil // drop this since it's expected in this case
				}
				// note that we don't filter "accept" errors when the context has not been canceled
				// as that is not expected to happen
			}
			srvErr = err
			srvErrWasSet = true // async shenanigans; see note on declaration
		}
	}()

	return serverRef, nil
}
*/
