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

	// SystemServiceName should be used when interfacing with
	// the host system's service management interface.
	SystemServiceName = "filesystem"

	// SystemServiceDisplayName is an alias for SystemServiceName
	// without character constraints.
	SystemServiceDisplayName = "File system service"

	// ServerRootName defines a name which servers and clients may use
	// to refer to the service in namespace oriented APIs.
	ServerRootName = "fs"
	// ServerName defines a name which servers and clients may use
	// to form or find connections to a named server instance.
	// (E.g. a Unix socket of path `.../$ServerRootName/$ServerName`.)
	ServerName = "server"

	// TODO: move to ipc/env/service?
	// TODO: document + names + protocol
	// These are non-text values to synchronize through other means (like JSON)
	_ ServiceStatus = iota
	ServiceStarting
	ServiceReady
	ServiceError
)
