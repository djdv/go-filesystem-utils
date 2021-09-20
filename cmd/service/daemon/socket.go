package daemon

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// maybeGetUnixSocketPath returns the path
// of the first Unix domain socket within the multiaddr (if any).
func maybeGetUnixSocketPath(ma multiaddr.Multiaddr) (target string, hadUnixComponent bool) {
	multiaddr.ForEach(ma, func(comp multiaddr.Component) bool {
		if hadUnixComponent = comp.Protocol().Code == multiaddr.P_UNIX; hadUnixComponent {
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

func listen(serviceMaddrs ...multiaddr.Multiaddr) ([]manet.Listener, error) {
	serviceListeners := make([]manet.Listener, len(serviceMaddrs))
	for i, maddr := range serviceMaddrs {
		listener, err := manet.Listen(maddr)
		if err != nil {
			err = fmt.Errorf("could not create service listener for %v: %w",
				maddr, err)
			// On failure, close what we opened so far.
			for _, listener := range serviceListeners[:i] {
				if lErr := listener.Close(); lErr != nil {
					err = fmt.Errorf("%w - could not close %s: %s",
						err, listener.Multiaddr(), lErr)
				}
			}
			return nil, err
		}
		serviceListeners[i] = listener
	}
	return serviceListeners, nil
}
