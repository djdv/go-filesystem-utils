package daemon

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"

	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/p9/p9"
)

// Servers and clients should use these values to coordinate
// connections between each other.
const (
	// ServiceName defines a name which clients may use
	// to refer to the service in namespace oriented APIs.
	// E.g. a socket's parent directory.
	ServiceName = "fs"

	// ServerName defines a name which clients may use
	// to form or find connections to a named server instance.
	// E.g. a socket of path `/$ServiceName/$ServerName`.
	ServerName = "server"

	// ServerCommandName defines a name which clients may use
	// to invoke the server process.
	ServerCommandName = "daemon"
)

// Servers and clients should use these flag values
// when spawning or summoning the daemon at the process level.
const (
	// FlagPrefix should be prepended to all flag names
	// that relate to the `fs` service itself.
	FlagPrefix = "api-"

	// FlagServer is used by server and client commands
	// to specify the listening channel;
	// typically a socket multiaddr.
	FlagServer = FlagPrefix + "server"

	// FlagExitAfter is used by server and client commands
	// to specify the idle check interval for the server.
	// Client commands will relay this to server instances
	// if they spawn one. Otherwise it is ignored (after parsing).
	FlagExitAfter = FlagPrefix + "exit-after"
)

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
