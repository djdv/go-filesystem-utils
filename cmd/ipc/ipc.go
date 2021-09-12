package ipc

import "github.com/djdv/go-filesystem-utils/cmd/formats"

type (
	// TODO: [Ame] Finalize protocols + English in documentation.
	// Probably best to do this in a human document and reference it here.
	// E.g. `IPC protocol.svg`
	//
	// The data representation does not matter as long as servers and clients understand them.
	// Only the sequence is important.
	// Our implementation uses Go structures in memory,
	// JSON over HTTP, and plain text over stdio, to coordinate between
	// client and server processes - depending on the context/request.
	/*
		Sequence init      = Starting;
		Sequence listeners = Starting, {Listener};
		Sequence end       = Ready;
		Sequence           = Sequence init, {Sequence listeners}, Sequence end;
	*/
	ServiceStatus   uint
	ServiceResponse struct {
		Status        ServiceStatus      `json:",omitempty"`
		ListenerMaddr *formats.Multiaddr `json:",omitempty"`
		Info          string             `json:",omitempty"`
	}
)

const (
	// Servers and clients should use these values as various rally points.
	// Where servers expose them and clients look for them in relevant APIs.

	// TODO: This has to change (to `daemon`, and needs a sister function.
	// ServiceCommandPath() { return []string{argv[0], "service", "daemon"} }
	// Maybe declared in the daemon pkg?
	// Some of these should be dropped like the display name
	// The stdio and status values should probably go into a sub-pkg too.
	// Just keep everything explicitly separate.
	//
	// TODO: [Ame] English.
	// ServiceCommandName should be used as the name for commands
	// which expose the file system commands API.
	// Typically the final component in the command line, HTTP path, etc. before arguments.
	// E.g. `programName someSubcommand $ServiceCommandName --parameter="argument"`,
	// `/someSubCommand/$ServiceCommandName?parameter=argument`, etc.
	ServiceCommandName = "service"

	ServiceDescription = "Manages active file system requests and instances."

	// ServiceName is a generic short-and-safe name which describes the purpose of the service.
	// Some APIs have restrictions on whitespace, symbols, character sets, etc.
	// This name may be used in these APIs. (E.g. as an identifier within an init system)
	// If necessary, length and character case should be adapted
	// to conform to the API standard being targeted. (E.g. shortened and cased as `fs`)
	ServiceName = "FileSystem"

	// ServiceDisplayName is the unrestricted / human friendly version of ServiceName.
	ServiceDisplayName = "File system service"

	// TODO: document + names
	// These values are expected to be printed on stdout by service servers.
	// TODO: the stdio protocol explanation here; header first; anything; ready
	// errors go to stderr.
	StdHeader     = ServiceDisplayName + " starting..."
	StdGoodStatus = "Listening on: "
	StdReady      = ServiceDisplayName + " started"

	// TODO: move to ipc/env/service?
	// TODO: document + names + protocol
	// These are non-text values to synchronize through other means (like JSON)
	_ ServiceStatus = iota
	ServiceStarting
	ServiceReady
	ServiceError
)
