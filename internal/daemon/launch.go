package daemon

import (
	"path"
	"path/filepath"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/multiformats/go-multiaddr"
)

const (
	ErrCouldNotConnect = generic.ConstError("could not connect to remote API")
	ErrServiceNotFound = generic.ConstError("could not find service instance")
)

func SelfConnect(args []string, options ...ClientOption) (*Client, error) {
	const defaultDecay = 6 * time.Second
	cmd, err := selfCommand(args, defaultDecay)
	if err != nil {
		return nil, err
	}

	sio, err := setupStdioIPC(cmd)
	if err != nil {
		return nil, err
	}

	serviceMaddr, err := startAndCommunicateWith(cmd, sio)
	if err != nil {
		return nil, err
	}

	return Connect(serviceMaddr, options...)
}

func servicePathsToServiceMaddrs(servicePaths ...string) ([]multiaddr.Multiaddr, error) {
	var (
		serviceMaddrs = make([]multiaddr.Multiaddr, 0, len(servicePaths))
		multiaddrSet  = make(map[string]struct{}, len(servicePaths))
	)
	for _, servicePath := range servicePaths {
		if _, alreadySeen := multiaddrSet[servicePath]; alreadySeen {
			continue // Don't return duplicates in our slice.
		} else {
			multiaddrSet[servicePath] = struct{}{}
		}
		maddrString := path.Join("/unix/",
			filepath.Join(servicePath, ServerRootName, ServerName))
		serviceMaddr, err := multiaddr.NewMultiaddr(maddrString)
		if err != nil {
			return nil, err
		}
		serviceMaddrs = append(serviceMaddrs, serviceMaddr)
	}
	return serviceMaddrs, nil
}
