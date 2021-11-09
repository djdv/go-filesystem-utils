package host

import (
	"errors"

	manet "github.com/multiformats/go-multiaddr/net"
)

// TODO:
// HostListeners takes in any user supplied listeners (optional)
// and retrieves any listeners provided by the host service manager.
// The cleanup function returned should be called after all listeners are closed.
func HostListeners(providedListeners []manet.Listener) (serviceListeners []manet.Listener,
	cleanup func() error, err error) {
	err = errors.New("NIY")
	return
}
