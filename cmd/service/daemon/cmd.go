package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	stopcmd "github.com/djdv/go-filesystem-utils/cmd/service/daemon/stop"
	"github.com/djdv/go-filesystem-utils/filesystem"
	cmdsenv "github.com/djdv/go-filesystem-utils/internal/cmds/environment"
	"github.com/djdv/go-filesystem-utils/internal/cmds/environment/stop"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings"
	"github.com/djdv/go-filesystem-utils/internal/generic"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

const Name = "daemon"

type Settings = settings.Root

// Command returns an instance of the `daemon` command.
func Command() *cmds.Command {
	return &cmds.Command{
		Helptext: cmds.HelpText{
			Tagline: "Manages file system requests and instances.",
		},
		NoRemote: true,
		PreRun:   daemonPreRun,
		Run:      daemonRun,
		Encoders: settings.Encoders,
		Type:     Response{},
		Subcommands: map[string]*cmds.Command{
			stopcmd.Name: stopcmd.Command,
		},
	}
}

// TODO: remove/replace this where used.
// CmdsPath returns the leading parameters
// to invoke the daemon's `Run` method from `main`.
func CmdsPath() []string { return []string{"service", Name} }

func daemonPreRun(*cmds.Request, cmds.Environment) error {
	return filesystem.RegisterPathMultiaddr()
}

func daemonRun(request *cmds.Request, emitter cmds.ResponseEmitter, env cmds.Environment) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	settings, serviceEnv, err := parseCmds(ctx, request, env)
	if err != nil {
		return err
	}

	daemonCtx, daemonCancel := context.WithCancel(ctx)
	defer daemonCancel()
	muEmitter, emitErrs, err := synchronizeWithStdio(daemonCtx,
		emitter,
		os.Stdin, os.Stdout, os.Stderr,
	)
	if err != nil {
		return err
	}

	var (
		daemonPath = request.Path
		stopPath   = append(daemonPath, stopcmd.Name)
	)
	stopperResponses, stopperReasons, err := setupStopperAPI(ctx, stopPath, serviceEnv)
	if err != nil {
		return err
	}
	// TODO: names
	goResponses, goErrs := setupGoStoppers(daemonCtx, request, serviceEnv.Stopper())

	listeners, listenErrs, err := settingsToListeners(ctx, request, settings)
	if err != nil {
		return err
	}
	var (
		serverListeners, serveErrs   = generateServers(ctx, request, serviceEnv, listeners)
		servers                      = startServers(ctx, serverListeners)
		serverResponses, serverCache = respondAndCache(ctx, servers)

		respond   = func(response *Response) error { return muEmitter.Emit(response) }
		responses = []responses{
			goResponses,
			stopperResponses,
			serverResponses,
		}
		errs = []errCh{
			goErrs,
			emitErrs,
			listenErrs,
			serveErrs,
		}

		exitCheckInterval = settings.AutoExitInterval
		exitWhenIdle      = exitCheckInterval != 0
	)
	if exitWhenIdle {
		idleResponses, idleErrs := stopOnIdleEvent(daemonCtx, serviceEnv, settings.AutoExitInterval)
		responses = append(responses, idleResponses)
		errs = append(errs, idleErrs)
	}

	var (
		emitResponses = func() error {
			if err := respond(startingResponse()); err != nil {
				return err
			}
			responses := generic.CtxMerge(ctx, responses...)
			return generic.ForEachOrError(ctx, responses, nil, respond)
		}

		stopReason stop.Reason
		wait       = func() error {
			var (
				finalErrs        = generic.CtxMerge(ctx, errs...)
				cancelAndRespond = func(reason stop.Reason) error {
					daemonCancel()
					stopReason = reason
					response := stoppingResponse(reason)
					if err := respond(response); err != nil {
						return err
					}
					return nil
				}
			)
			return generic.ForEachOrError(ctx, stopperReasons, finalErrs, cancelAndRespond)
		}
	)

	if err := emitResponses(); err != nil {
		return err
	}

	if err := wait(); err != nil {
		stopReason = stop.Error
		// TODO: wrap err?
		respond(stoppingResponse(stopReason))
		return err
	}

	shutdown := func(ctx context.Context, servers <-chan serverInstance) error {
		const shutdownGrace = 30 * time.Second
		var (
			shutdownMaddrs, shutdownErrs = shutdownServers(ctx, shutdownGrace, servers)
			broadcastShutdown            = func(maddr multiaddr.Multiaddr) error {
				return respond(maddrShutdownResponse(maddr, stopReason))
			}
		)
		return generic.ForEachOrError(ctx, shutdownMaddrs, shutdownErrs, broadcastShutdown)
	}
	return handleStderr(shutdown(ctx, serverCache))
}

func parseCmds(ctx context.Context, request *cmds.Request,
	env cmds.Environment,
) (*Settings, cmdsenv.Environment, error) {
	daemonSettings, err := settings.Parse[Settings](ctx, request)
	if err != nil {
		return nil, nil, err
	}
	serviceEnv, err := cmdsenv.Assert(env)
	if err != nil {
		return nil, nil, err
	}
	return daemonSettings, serviceEnv, nil
}

func settingsToListeners(ctx context.Context,
	request *cmds.Request, settings *Settings,
) (listeners, errCh, error) {
	var (
		listeners = make([]listeners, 0, 3)
		errChans  = make([]errCh, 0, 1)
	)
	hostListeners, err := hostListenersFromRequest(request)
	if err != nil {
		return nil, nil, err
	}
	if hostListeners != nil {
		listeners = append(listeners, generate(ctx, hostListeners...))
	}

	serviceMaddrs := settings.ServiceMaddrs
	if serviceMaddrs != nil {
		var (
			maddrs                = generate(ctx, serviceMaddrs...)
			argListeners, argErrs = listenersFromMaddrs(ctx, maddrs)
		)
		listeners = append(listeners, argListeners)
		errChans = append(errChans, argErrs)
	}

	useDefaults := len(listeners) == 0
	if useDefaults {
		userMaddrs, err := UserServiceMaddrs()
		if err != nil {
			return nil, nil, err
		}
		filteredMaddrs, err := filterUnixMaddrs(userMaddrs...)
		if err != nil {
			return nil, nil, err
		}
		var (
			maddrs                        = generate(ctx, filteredMaddrs...)
			defaultListeners, defaultErrs = initializeAndListen(ctx, maddrs)
		)
		listeners = append(listeners, defaultListeners)
		errChans = append(errChans, defaultErrs)
	}
	return generic.CtxMerge(ctx, listeners...), generic.CtxMerge(ctx, errChans...), nil
}

func filterDefaultPaths(paths ...string) ([]string, error) {
	var (
		filtered = make([]string, 0, len(paths))
		pathSet  = make(map[string]struct{}, len(paths))
		exist    = func(path string) bool {
			_, err := os.Stat(path)
			return !os.IsNotExist(err)
		}
	)
	// HACK: we could probably convert and copy less.
	// pair index to maddr, check path, don't reconstruct maddr.
	for _, path := range paths {
		socketRoot, socketDir := splitSocketPath(path)
		if _, alreadySeen := pathSet[path]; alreadySeen {
			continue // Don't return duplicates in our slice.
		}
		pathSet[path] = struct{}{}

		if !exist(socketRoot) {
			continue
		}
		if exist(socketDir) {
			continue
		}

		filtered = append(filtered, path)
	}
	if len(filtered) == 0 {
		// TODO: fallback to TCP / build constrained default
		return nil, errors.New("can't determine which default socket to use")
	}
	// We only need to serve on one.
	// TODO: none of this is obvious by function name,
	// we need to split up and restructure.
	return filtered[:1], nil
}

func splitSocketPath(path string) (root, dir string) {
	dir = filepath.Dir(path)
	return filepath.Dir(dir), dir
}

func filterUnixMaddrs(maddrs ...multiaddr.Multiaddr) ([]multiaddr.Multiaddr, error) {
	relay := make([]multiaddr.Multiaddr, 0, len(maddrs))
	paths := make([]string, 0, len(maddrs))
	for _, maddr := range maddrs {
		path := getFirstUnixSocketPath(maddr)
		if path == "" {
			relay = append(relay, maddr)
		} else {
			paths = append(paths, path)
		}
	}
	filteredPaths, err := filterDefaultPaths(paths...)
	if err != nil {
		return nil, err
	}
	filtered, err := pathsToUnixMaddrs(filteredPaths...)
	if err != nil {
		return nil, err
	}
	return append(relay, filtered...), nil
}
