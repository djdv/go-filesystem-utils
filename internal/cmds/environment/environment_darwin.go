package cmdsenv

import "github.com/kardianos/service"

func systemListeners(maddrsProvided bool, _ service.Logger) (serviceListeners []manet.Listener,
	cleanup func() error, err error,
) {
	// TODO: pull from service config keyvalue
	socketPath = "/var/run/fsservice"
	// Not implemented yet
}
