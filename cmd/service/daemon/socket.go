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
		results = mergeListenerResults(results, argListeners)
	}
	return results
}

func maddrsToChan(ctx context.Context,
	maddrs ...multiaddr.Multiaddr) <-chan multiaddr.Multiaddr {
	maddrChan := make(chan multiaddr.Multiaddr, len(maddrs))
	go func() {
		defer close(maddrChan)
		for _, maddr := range maddrs {
			if ctx.Err() != nil {
				return
			}
			maddrChan <- maddr
		}
	}()
	return maddrChan
}

func listenersFromMaddrs(ctx context.Context,
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

// TODO: splitup more; specifically the relation between maddrs, listeners, and sockdirs
// we want to go from maddr -> listener directly
// not listener -> listener (at least here)
// So maddr -> needsPrep? -> prepper -> listener -> here
// So maddr -> doesn'tNeedsPrep? -> listener -> here
// if cancled now, close listener
// in the other loops do the same pattern implicitly.
//
// TODO: doc or remove
// (if required) a cleanup function will run at end of each listeners close method
func setupAndListen(ctx context.Context,
	maddrs <-chan multiaddr.Multiaddr) <-chan listenResult {
	var (
		results = make(chan listenResult, cap(maddrs))
		sendErr = func(err error) { results <- listenResult{error: err} }
	)
	go func() {
		defer close(results)
		for maddr := range maddrs {
			if ctx.Err() != nil {
				fmt.Println("DBG: listen did nothing before cancel")
				return
			}
			var (
				socketCleanup       cleanupFunc
				maybeCleanupAndSend = func(err error) {
					if socketCleanup != nil {
						cErr := socketCleanup()
						if cErr != nil {
							cErr = fmt.Errorf("could not cleanup for socket: %w", cErr)
						}
						if err == nil {
							err = cErr
						} else {
							err = fmt.Errorf("%w\n\t%s", err, cErr)
						}
					}
					if err != nil {
						sendErr(err)
					}
				}
				unixSocketPath = getFirstUnixSocketPath(maddr)
				sockHasUDSPath = unixSocketPath != ""
			)
			if sockHasUDSPath {
				socketDir := filepath.Dir(unixSocketPath)
				rmDir, err := makeDir(socketDir)
				if err != nil {
					maybeCleanupAndSend(err)
					continue
				}
				socketCleanup = rmDir
			}
			if ctx.Err() != nil {
				fmt.Println("DBG: listen made dir before cancel")
				maybeCleanupAndSend(nil)
				return
			}

			listener, err := manet.Listen(maddr)
			if err != nil {
				maybeCleanupAndSend(err)
				continue
			}
			if socketCleanup != nil {
				listener = listenerWithCleanup{
					Listener:    listener,
					cleanupFunc: socketCleanup,
				}
			}
			socketCleanup = listener.Close

			if ctx.Err() != nil {
				fmt.Println("DBG: listen made listener before cancel")
				maybeCleanupAndSend(err)
				return
			}
			results <- listenResult{Listener: listener}
		}
	}()

	return results
}

func makeDir(path string) (rmDir cleanupFunc, err error) {
	if err = os.Mkdir(path, 0o775); err != nil {
		return
	}
	// rmDir = func() error { return os.Remove(path) }
	rmDir = func() error {
		fmt.Println("rmDir called from:")
		for i := 1; ; i++ {
			_, f, l, ok := runtime.Caller(i)
			if !ok {
				break
			}
			f = filepath.Base(f)
			fmt.Println("\t", f, l)
		}
		return os.Remove(path)
	}
	return
}

type (
	cleanupFunc func() error

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

func bridgeListenerResults(input <-chan <-chan listenResult,
) <-chan listenResult {
	output := make(chan listenResult)
	go func() {
		defer close(output)
		for listenResults := range input {
			for listenResult := range listenResults {
				output <- listenResult
			}
		}
	}()
	return output
}

func mergeListenerResults(car, cdr <-chan listenResult) <-chan listenResult {
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
	if listeners := listenersFromCmds(ctx, request, maddrs...); listeners != nil {
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
func listenersFromCmdExtra(_ context.Context,
	cmdsExtra *cmds.Extra) <-chan listenResult {
	cmdListeners, provided := cmdsExtra.GetValue("magic")
	if !provided {
		return nil
	}

	listeners, ok := cmdListeners.([]manet.Listener)
	if !ok {
		singleErr := func(err error) <-chan listenResult {
			singleErr := make(chan listenResult, 1)
			singleErr <- listenResult{error: err}
			close(singleErr)
			return singleErr
		}

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
	results := make(chan listenResult, len(listeners))
	go func() {
		defer close(results)
		for _, listener := range listeners {
			results <- listenResult{Listener: listener}
		}
	}()

	return results
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
