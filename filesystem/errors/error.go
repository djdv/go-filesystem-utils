package errors

type (
	Kind  uint8
	Error interface {
		error
		Kind() Kind
	}
)

// TODO: put a remark about this somewhere; probably in /transform/filesystems/???.go docs
// the intermediate operations that uses these errors aren't exactly the most truthful
// Kind biases towards POSIX errors in intermediate operations
// an example of this is Permission being returned from intermediate.Remove when called on the wrong type
// despite the fact this should really be ErrorInvalid
// another is Other being returned in Info for the same reason

//go:generate stringer -type=Kind
const ( // kind subset kindly borrowed from rob
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
