package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/djdv/go-filesystem-utils/cmd/environment"
	"github.com/djdv/go-filesystem-utils/cmd/fs/settings"
	. "github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

type (
	errCh = <-chan error

	Settings struct {
		settings.Settings
	}
)

func (*Settings) Parameters() parameters.Parameters {
	return (*settings.Settings)(nil).Parameters()
}

func parseCmds(ctx context.Context, request *cmds.Request,
	env cmds.Environment) (*Settings, environment.Environment, error) {
	daemonSettings, err := settings.ParseAll[Settings](ctx, request)
	if err != nil {
		return nil, nil, err
	}
	serviceEnv, err := environment.Assert(env)
	if err != nil {
		return nil, nil, err
	}
	return daemonSettings, serviceEnv, nil
}

func settingsToListeners(ctx context.Context,
	request *cmds.Request, settings *Settings) (listeners, errCh, error) {
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
	return CtxMerge(ctx, listeners...), CtxMerge(ctx, errChans...), nil
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
