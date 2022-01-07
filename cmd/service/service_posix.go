//go:build !windows && !darwin
// +build !windows,!darwin

package service

import (
	"net"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	"github.com/coreos/go-systemd/activation"
	fscmds "github.com/djdv/go-filesystem-utils/cmd/filesystem"
	"github.com/kardianos/service"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

func systemListeners(maddrsProvided bool, sysLog service.Logger) (serviceListeners []manet.Listener,
	cleanup func() error, err error) {
	defer func() { // NOTE: Overwrites named return value.
		if err != nil {
			err = logErr(sysLog, err)
		}
	}()

	// FIXME: we need to use activation.Files(false) here; otherwise we'll lose these on restart.
	// That is, service restart, not process restart.
	//*Right now those are the same thing so it doesn't matter, but it might later.
	var systemListeners []net.Listener
	if systemListeners, err = activation.Listeners(); err != nil {
		return
	}

	serviceListeners = make([]manet.Listener, len(systemListeners))
	for i, listener := range systemListeners {
		var cast manet.Listener
		if cast, err = manet.WrapNetListener(listener); err != nil {
			return
		}
		serviceListeners[i] = cast
	}
	if len(serviceListeners) > 0 {
		return
	}

	// Nothing provided by the system, make our own.
	var (
		socketPath   string
		serviceMaddr multiaddr.Multiaddr
	)
	socketPath, err = xdg.ConfigFile(filepath.Join(fscmds.ServiceName, fscmds.ServerName))
	if err != nil {
		return
	}
	cleanup = func() error { return os.Remove(filepath.Dir(socketPath)) }

	multiaddrString := "/unix/" + socketPath
	if serviceMaddr, err = multiaddr.NewMultiaddr(multiaddrString); err != nil {
		return
	}
	serviceListeners, err = listen(serviceMaddr)

	return
}
