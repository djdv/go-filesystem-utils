package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type listenResult struct {
	manet.Listener
	error
}

func listenersFromCmds(ctx context.Context, request *cmds.Request,
	maddrs ...multiaddr.Multiaddr) <-chan listenResult {
	var (
		cmdsListeners = listenersFromCmdExtra(ctx, request.Command.Extra)
		results       = cmdsListeners
	)
	if len(maddrs) != 0 {
		argListeners := listenersFromMaddrs(ctx, maddrsToChan(ctx, maddrs...))
		if results == nil {
			results = argListeners
		}
		results = joinListenerResults(results, argListeners)
	}
	return results
}

func maddrsToChan(ctx context.Context,
	maddrs ...multiaddr.Multiaddr) <-chan multiaddr.Multiaddr {
	// maddrChan := make(chan multiaddr.Multiaddr, len(maddrs))
	maddrChan := make(chan multiaddr.Multiaddr)
	go func() {
		defer close(maddrChan)
		for _, maddr := range maddrs {
			select {
			case maddrChan <- maddr:
			case <-ctx.Done():
				return
			}
		}
	}()
	return maddrChan
}

func listenersToResults(ctx context.Context,
	listeners ...manet.Listener) <-chan listenResult {
	// listenerChan := make(chan multiaddr.Multiaddr, len(listeners))
	listenerChan := make(chan listenResult)
	go func() {
		defer close(listenerChan)
		for _, listener := range listeners {
			select {
			case listenerChan <- listenResult{Listener: listener}:
			case <-ctx.Done():
				fmt.Println("👀 closing via ctx")
				err := listener.Close()
				fmt.Println("👀 closing via ctx err:", err)
				return
			}
		}
	}()
	return listenerChan
}

func listenersFromMaddrs(ctx context.Context,
	maddrs <-chan multiaddr.Multiaddr) <-chan listenResult {
	// results := make(chan listenResult, cap(maddrs))
	results := make(chan listenResult)
	go func() {
		defer close(results)
		for maddr := range maddrs {
			var result listenResult
			result.Listener, result.error = manet.Listen(maddr)
			select {
			case results <- result:
			case <-ctx.Done():
				result.Close()
				return
			}
		}
	}()
	return results
}

func somethingHost(ctx context.Context,
	maddrs <-chan multiaddr.Multiaddr) <-chan listenResult {
	results := make(chan listenResult, cap(maddrs))
	go func() {
		defer close(results)
		for maddr := range maddrs {
			var result listenResult
			result.Listener, result.error = manet.Listen(maddr)
			select {
			case results <- result:
			case <-ctx.Done():
				result.Close()
				return
			}
		}
	}()
	return results
}

type mkDirResult struct {
	rmDir cleanupFunc
	error
}

func makeDir(ctx context.Context, paths <-chan string) <-chan mkDirResult {
	results := make(chan mkDirResult, cap(paths))
	go func() {
		defer close(results)
		for path := range paths {
			if err := os.Mkdir(path, 0o775); err != nil {
				results <- mkDirResult{error: err}
			} else {
				cleanup := func() error { return os.Remove(path) }
				results <- mkDirResult{rmDir: cleanup}
			}
		}
	}()
	return results
}

/*
func prepareHostForSocket(maddr multiaddr.Multiaddr) (cleanupFunc, error) {
	unixSocketPath := getFirstUnixSocketPath(maddr)

	// TODO: switch this back to `os.Stat` when this is merged:
	// https://go-review.googlesource.com/c/go/+/338069/
	if _, err := os.Lstat(unixSocketPath); err == nil {
		return nil, fmt.Errorf(
			"socket file already exists: \"%s\"",
			unixSocketPath)
	}

	socketDir := filepath.Dir(unixSocketPath)
	if err := os.Mkdir(socketDir, 0o775); err != nil {
		return nil, fmt.Errorf(
			"can't create socket's parent directory"+
				"\n\t(\"%s\")"+
				"\n\t%w",
			unixSocketPath, err)
	}
	// cleanup := func() error { return os.Remove(socketDir) }
	cleanup := func() error {
		err := os.Remove(socketDir)
		fmt.Println("DBG: cleanup err for:\n\t", socketDir, "\n\t", err)
		return err
	}

	fmt.Println("DBG: made:\n\t", socketDir)

	return cleanup, nil
}
*/

// for each maddr, (if required)
// a cleanup function will run at end of the listeners close method.
func setupAndListen(ctx context.Context,
	maddrs <-chan multiaddr.Multiaddr) <-chan listenResult {
	var (
		mkDirs  = make(chan string, 1)
		dirErrs = makeDir(ctx, mkDirs)

		mkListeners = make(chan multiaddr.Multiaddr, 1)
		listeners   = listenersFromMaddrs(ctx, mkListeners)

		// results = make(chan listenResult, cap(maddrs))
		results = make(chan listenResult, 1)
	)
	go func() {
		defer close(results)
		defer fmt.Println("DBG: closing results")
		for maddr := range maddrs {
			fmt.Println("🌩️ DBG: processing:", maddr)
			// Earliest cancel - nothing to be done.
			if ctx.Err() != nil {
				fmt.Println("DBG: listen did nothing before cancel")
				return
			}
			var (
				result listenResult

				unixSocketPath = getFirstUnixSocketPath(maddr)
				sockHasUDSPath = unixSocketPath != ""

				socketCleanup cleanupFunc
				wrapCleanup   = func(err error) error {
					if socketCleanup != nil {
						if cErr := socketCleanup(); cErr != nil {
							cErr := fmt.Errorf("could not cleanup for socket: %w", cErr)
							if err == nil {
								err = cErr
							} else {
								err = fmt.Errorf("%w\n\tcould not cleanup: %s",
									err, cErr)
							}
						}
					}
					return err
				}
			)
			if sockHasUDSPath {
				socketDir := filepath.Dir(unixSocketPath)
				mkDirs <- socketDir
				dirResult := <-dirErrs
				if err := dirResult.error; err != nil {
					err = fmt.Errorf(
						"can't create socket's parent directory"+
							"\n\t(\"%s\")"+
							"\n\t%w",
						unixSocketPath, err)
					result.error = err
					results <- result
					return
				}
				fmt.Println("DBG: made:", socketDir)
				// socketCleanup = dirResult.rmDir
				socketCleanup = func() error {
					err := dirResult.rmDir()
					fmt.Println("DBG: removed:", socketDir, err)
					return err
				}
				if ctx.Err() != nil {
					fmt.Println("DBG: listen made dir before cancel, cleaning now")
					// Socket prep cancel - remove host state made above.
					if err := wrapCleanup(nil); err != nil {
						result.error = err
						results <- result
					}
					return
				}
			}

			mkListeners <- maddr
			listener, ok := <-listeners
			if !ok {
				fmt.Println("DBG: (the other one) listen made dir before cancel, cleaning now")
				if err := wrapCleanup(nil); err != nil {
					result.error = err
					results <- result
				}
				return
			}
			fmt.Printf("DBG: listening: %#v\n", listener)
			if err := listener.error; err != nil {
				result.error = wrapCleanup(err)
				results <- listener
				return // TODO: continue? (only if there's more than 1 interfaces?(`cap(input)`))
				// log failures to console and let caller decide what to do.
			}
			if socketCleanup != nil {
				listener = listenResult{
					Listener: listenerWithCleanup{
						Listener:    listener.Listener,
						cleanupFunc: socketCleanup,
					},
				}
			}

			// Last possible abort - listener must be closed (cascades cleanup)
			if err := ctx.Err(); err != nil {
				lErr := listener.Close()
				if lErr != nil {
					err = fmt.Errorf("%w\n\tcould not close listener: %s", err, lErr)
				}
				fmt.Println("DBG: listen  aborted after listen:", err)
				result.Listener = nil
				result.error = err
				return
			}

			results <- listener
			fmt.Println("DBG: listener sent")
		}
	}()

	return results
}

type (
	cleanupFunc         func() error
	listenerWithCleanup struct {
		manet.Listener
		cleanupFunc
	}
)

func (listener listenerWithCleanup) Close() error {
	err := listener.Listener.Close()
	if err != nil {
		err = fmt.Errorf("could not close listener: %w", err)
	}
	cleanup := listener.cleanupFunc
	if cleanup == nil {
		return err
	}

	cErr := cleanup()
	if cErr != nil {
		cErr = fmt.Errorf("could not cleanup after listener: %w", err)
		if err == nil {
			return cErr
		}
		return fmt.Errorf("%w\n\t%s", err, cErr)
	}
	return err
}

func joinListenerResults(car, cdr <-chan listenResult) <-chan listenResult {
	combined := make(chan listenResult, cap(car)+cap(cdr))
	go func() {
		defer close(combined)
		for v := range car {
			combined <- v
		}
		for v := range cdr {
			combined <- v
		}
	}()
	return combined
}

func getListeners(ctx context.Context, request *cmds.Request,
	maddrs ...multiaddr.Multiaddr) <-chan listenResult {
	listeners := listenersFromCmds(ctx, request, maddrs...)
	if listeners != nil {
		return listeners
	}
	return defaultListeners(ctx)
}

// TODO: We need a way to pass listeners from `service` to `daemon`.
// Easiest way would be to add a parameter for it of type `[]net.Listener`.
// This would need to be of typed as `[]string` in the cmds.Option,
// which could be in a specific format.
// Anything usable with `os.NewFile`->`net.FileListener`.
// E.g.
// CSV of `file-descriptor $groupseperator  file-name`
// As `fs.exe service daemon --existing-sockets="3:systemdSock1,4:systemdSock2"`
//
// For now, we can sidestep this at the API level and just use command.Extra
// (from within `service`, after copying `daemon` add the listeners, then call execute on that)
// But this is not a proper solution, it's only temporary to not break the existing feature
// while separating the commands implementations.
func listenersFromCmdExtra(ctx context.Context,
	cmdsExtra *cmds.Extra) <-chan listenResult {
	singleErr := func(err error) <-chan listenResult {
		singleErr := make(chan listenResult, 1)
		singleErr <- listenResult{error: err}
		close(singleErr)
		return singleErr
	}

	cmdListeners, provided := cmdsExtra.GetValue("magic")
	if !provided {
		return nil
	}

	listeners, ok := cmdListeners.([]manet.Listener)
	if !ok {
		return singleErr(fmt.Errorf(
			"Command.Extra value has wrong type"+
				"\n\texpected %T"+
				"\n\tgot: %T",
			listeners,
			cmdListeners,
		))
	}
	// TODO: replace this direct copy to something like (fd=>listener)
	// caller passes as --someArg=fd1:unix,fd2:tcp,...
	// maybe --unix-listeners=fd1,--tcp-listeners=fd2,...
	// the caller would parse this and pass it to us.
	// Like (unix,tcp,udp []fdint) or something.
	// NOTE: Currently, no error is possible
	// since listeners are passed and asserted directly.
	// This will likely change when going from
	// fd (int) -> go interface assertions, done for each fd.
	relay := listenersToResults(ctx, listeners...)

	return relay
}

func defaultListeners(ctx context.Context) <-chan listenResult {
	maddrs, err := UserServiceMaddrs()
	if err != nil {
		single := make(chan listenResult, 1)
		single <- listenResult{error: err}
		close(single)
		return single
	}
	return setupAndListen(ctx, maddrsToChan(ctx, maddrs...))
}

// getFirstUnixSocketPath returns the path
// of the first Unix domain socket within the multiaddr (if any)
func getFirstUnixSocketPath(ma multiaddr.Multiaddr) (target string) {
	multiaddr.ForEach(ma, func(comp multiaddr.Component) bool {
		isUnixComponent := comp.Protocol().Code == multiaddr.P_UNIX
		if isUnixComponent {
			target = comp.Value()
			if runtime.GOOS == "windows" { // `/C:\path` -> `C:\path`
				target = strings.TrimPrefix(target, `/`)
			}
			return true
		}
		return false
	})
	return
}
