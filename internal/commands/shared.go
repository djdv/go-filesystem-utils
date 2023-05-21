package commands

import fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"

const (
	// serverRootName defines a name which servers and clients may use
	// to refer to the service in namespace oriented APIs.
	// E.g. a socket's parent directory.
	serverRootName = "fs"

	// serverName defines a name which servers and clients may use
	// to form or find connections to a named server instance.
	// E.g. a socket of path `/$ServerRootName/$serverName`.
	serverName = "server"

	// apiFlagPrefix should be prepended to all flag names
	// that relate to the `fs` service itself.
	apiFlagPrefix = "api-"

	// serverFlagName is used by server and client commands
	// to specify the listening channel;
	// typically a socket multiaddr.
	serverFlagName = apiFlagPrefix + "server"

	// exitAfterFlagName is used by server and client commands
	// to specify the idle check interval for the server.
	// Client commands will relay this to server instances
	// if they spawn one. Otherwise it is ignored (after parsing).
	exitAfterFlagName = apiFlagPrefix + "exit-after"

	// mountsFileName is the name used by servers
	// to host a [p9fs.MountFile].
	// All commands that interact with its protocol
	// should use this name to resolve the [p9.File].
	mountsFileName = "mounts"
)

func unwind(err error, funcs ...func() error) error {
	var errs []error
	for _, fn := range funcs {
		if fnErr := fn(); fnErr != nil {
			errs = append(errs, fnErr)
		}
	}
	if errs == nil {
		return err
	}
	return fserrors.Join(append([]error{err}, errs...)...)
}
