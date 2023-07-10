package commands

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

	// For names; all commands that interact with
	// that file's protocol should use appropriate
	// constants to resolve the [p9.File] via `Walk`.

	// mountsFileName is the name used by servers
	// to host a [p9fs.MountFile].
	mountsFileName = "mounts"

	// listenersFileName is the name used by servers
	// to host a [p9fs.Listener].
	listenersFileName = "listeners"

	// controlFileName is the name used by servers
	// to host 9P directory containing various server
	// control files.
	controlFileName = "control"

	// shutdownFileName is the name used by servers
	// to host a 9P file used to request shutdown,
	// by writing a [shutdownDisposition] (string or byte)
	// value to the file.
	shutdownFileName = "shutdown"
)
