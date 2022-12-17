package errors

import "io/fs"

type (
	// Kind specifies a type of error.
	Kind uint8

	// Error wraps a [fs.PathError]
	// with an error [Kind].
	Error struct {
		fs.PathError
		Kind
	}
)

//go:generate stringer -type=Kind
const (
	Other            Kind = iota // Unclassified error.
	InvalidItem                  // Invalid operation for the item being operated on.
	InvalidOperation             // Operation itself is not valid within the system.
	Permission                   // Permission denied.
	IO                           // External I/O error such as network failure.
	Exist                        // Item already exists.
	NotExist                     // Item does not exist.
	IsDir                        // Item is a directory.
	NotDir                       // Item is not a directory.
	NotEmpty                     // Directory not empty.
	ReadOnly                     // File system has no modification capabilities.
)

func (e *Error) Unwrap() error { return &e.PathError }
