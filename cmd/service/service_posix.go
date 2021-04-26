//go:build !windows
// +build !windows

package service

import (
	"net"
	"os"
	"path/filepath"
	"runtime"

	"github.com/adrg/xdg"
	"github.com/coreos/go-systemd/activation"
	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

func systemListeners(providedMaddrs ...multiaddr.Multiaddr) (serviceListeners []manet.Listener,
	cleanup func() error, err error) {
	if len(providedMaddrs) != 0 {
		if serviceListeners, err = listen(providedMaddrs...); err != nil {
			return
		}
	}

	// TODO: separate out for launchd/macos
	// FIXME: we need to use activation.Files(false) here; otherwise we'll lose these on restart.
	// That is, service restart, not process restart.
	//*Right now those are the same thing so it doesn't matter, but it will later.
	var systemListeners []net.Listener
	if systemListeners, err = activation.Listeners(); err != nil {
		return
	}

	providedListeners := make([]manet.Listener, len(systemListeners))
	for i, listener := range systemListeners {
		var cast manet.Listener
		if cast, err = manet.WrapNetListener(listener); err != nil {
			return
		}
		providedListeners[i] = cast
	}

	serviceListeners = append(serviceListeners, providedListeners...)

	var (
		socketPath   string
		serviceMaddr multiaddr.Multiaddr
	)
	if runtime.GOOS == "darwin" { // TODO: move to constrained file
		// TODO: pull from service config keyvalue
		socketPath = "/var/run/fsservice"
		return
	} else {
		socketPath, err = xdg.ConfigFile(filepath.Join(fscmds.ServiceName, fscmds.ServerName))
		if err != nil {
			return
		}
		cleanup = func() error { return os.Remove(filepath.Dir(socketPath)) }
	}

	multiaddrString := "/unix/" + socketPath
	if serviceMaddr, err = multiaddr.NewMultiaddr(multiaddrString); err != nil {
		return
	}
	serviceListeners, err = listen(serviceMaddr)

	return
}
