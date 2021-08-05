package ipc

const (
	// Servers and clients should use these values as various rally points.
	// Where servers expose them and clients look for them in relevant APIs.

	// ServiceCommandName is the API endpoint for the hosting command itself.
	// Typically the final component in the command line, HTTP path, etc. before arguments.
	// E.g. `program parentCommand service --parameter="argument"`,
	// `/parentCommand/service?parameter=argument`, etc.
	ServiceCommandName = "service"

	// ServiceDescription should be self explanatory ;^)
	ServiceDescription = "Manages active file system requests and instances."

	// ServiceName is a generic short-and-safe name which describes the purpose of the service.
	// Some APIs have restrictions on whitespace, symbols, character sets, etc.
	// This name may be used in these APIs. (E.g. as an identifier within an init system)
	// If necessary, length and character case should be adapted
	// to conform to the API standard being targeted. (E.g. shortened and cased as `fs`)
	ServiceName = "FileSystem"

	// ServiceDisplayName is the unrestricted / human friendly version of ServiceName.
	ServiceDisplayName = "File System Service"

	// NOTE: Used by the executor.
	// To synchronize with a service subprocess that outputs to StdErr.
	// TODO: document + names; this moved and was exported. Now needs to be canonized.
	StdHeader     = ServiceDisplayName + " starting..."
	StdGoodStatus = "Listening on: "
	StdReady      = ServiceDisplayName + " started"
)
