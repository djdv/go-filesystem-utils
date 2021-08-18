// TODO: copied from old branch paths: fs/intf/errors, fs/errors
// Currently in the process of merging and refactoring.
// Inspired by rob's writeup related to upspin,
// modified for our usecase,
// further modified (now/in-progress) to adapt-to/converge-with Go's new `fs` standard.
//
// Original docs for interface errors:
// Package errors wraps the fserrors, categorizing them in a way that allows
// translation between fs error value and host API error value.
//
// Original docs for fs errors:
// Package errors defines a common set of error annotations
// that implementations may use to translate Go errors into an external sentinel value.
package errors

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
)

// TODO: Currently we're using some of the upspin errors code as-is.
// But we need to write our own formatter for our own purpose
// since they differ enough to warrant a split versus an import.
// Right now things are copy pasted to get things building without looking too ugly.
// It's likely going to be similar, but not exact. Lots of opinion overlap.

type (
	// TODO: we should probably note why this exists ✔️
	// TODO: [Ame] English.
	// it's just a typedef for the constructor to switch on.
	// To distinguish between untyped strings.
	Op string

	// TODO: docs; same as Op
	Path string

	// TODO: [Ame] English.
	// Kind identifies the type of error it's classified under.
	Kind uint8

	// Error surprisingly implements Go's error interface.
	Error struct {
		fs.PathError
		Kind
	}
)

// pad appends str to the buffer if the buffer already has some data.
func pad(b *bytes.Buffer, str string) {
	if b.Len() == 0 {
		return
	}
	b.WriteString(str)
}

// TODO: replace - we shouldn't need something like this.
func (e *Error) isZero() bool {
	return e.Path == "" && e.Op == "" && e.Kind == 0 && e.Err == nil
}

// TODO: replace - we're not going to have dynamic separators at the pkg level,
// at least not like this.
//
// separator is the string used to separate nested errors. By
// default, to make errors easier on the eye, nested errors are
// indented on a new line. A server may instead choose to keep each
// error on a single line by modifying the separator string, perhaps
// to ":: ".
var separator = ":\n\t"

func (e *Error) Error() string {
	b := new(bytes.Buffer)
	if e.Op != "" {
		pad(b, ": ")
		b.WriteString(string(e.Op))
	}
	if e.Path != "" {
		pad(b, ": ")
		b.WriteString(string(e.Path))
	}
	if e.Kind != Other {
		pad(b, ": ")
		b.WriteString(e.Kind.String())
	}
	if e.Err != nil {
		// Indent on new line if we are cascading non-empty Upspin errors.
		if prevErr, ok := e.Err.(*Error); ok {
			if !prevErr.isZero() {
				pad(b, separator)
				b.WriteString(e.Err.Error())
			}
		} else {
			pad(b, ": ")
			b.WriteString(e.Err.Error())
		}
	}
	if b.Len() == 0 {
		return "no error"
	}
	return b.String()
}

// TODO: This comment was copied from an old branch and is outdated.
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

var ( // TODO: errors -> const strings mapped via kind
	errExist    = errors.New("already exists")
	errNotExist = errors.New("does not exist")
	errIsDir    = errors.New("not a file")
	errNotDir   = errors.New("not a directory")
	errNotEmpty = errors.New("directory is not empty")
	errReadOnly = errors.New("read only system")
)

func New(args ...interface{}) error {
	if len(args) == 0 {
		panic("call to errors.New with no arguments")
	}
	e := &Error{}
	for _, arg := range args {
		switch arg := arg.(type) {
		case Op:
			e.Op = string(arg)
		case Path:
			e.Path = string(arg)
		case string:
			e.PathError.Err = errors.New(arg)
		case Kind:
			e.Kind = arg
		case *Error:
			// We may modify this value
			// (we don't want to modify the original)
			copy := *arg
			e.PathError.Err = &copy
		case error:
			e.PathError.Err = arg
		default:
			panic(fmt.Errorf("unknown type %T, value %v in error call", arg, arg))
		}
	}

	prev, ok := e.PathError.Err.(*Error)
	if !ok {
		return e
	}

	// TODO: We most likely don't need this.

	// The previous error was also one of ours. Suppress duplications
	// so the message won't contain the same kind, file name or user name
	// twice.
	if prev.Kind == e.Kind {
		prev.Kind = Other
	}
	// If this error has Kind unset or Other, pull up the inner one.
	if e.Kind == Other {
		e.Kind = prev.Kind
		prev.Kind = Other
	}
	return e
}
