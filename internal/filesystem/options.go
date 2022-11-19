package filesystem

import "io/fs"

type (
	IPFSOption interface { // TODO: might not need this
		PinfsOption | KeyfsOption
	}

	PinfsOption func(*IPFSPinAPI) error
	KeyfsOption func(*IPFSKeyAPI) error
)

// TODO: try to eliminate this form of generic options.
// Standard still fights us, be I remember solving this for the 9P stuff
// in a way that wasn't too bad and could be rectified properly
// in later Go revisions.

// TODO: We need to nail down our hierarchy for interfaces
// + their definitions. And then decide how we want to handle it.
// Either the options should take in the fs.FS interface and assert it,
// or leverage the type system to check it at compile time
// (make the option constructors accept only our interfaces, not the broad standard one).

func WithIPFS[OT IPFSOption](ipfs fs.FS) (option OT) {
	switch fnPtrPtr := any(&option).(type) {
	case *PinfsOption:
		*fnPtrPtr = func(pa *IPFSPinAPI) error { pa.ipfs = ipfs; return nil }
	}
	return option
}

func WithIPNS[OT IPFSOption](ipns fs.FS) (option OT) {
	switch fnPtrPtr := any(&option).(type) {
	case *KeyfsOption:
		*fnPtrPtr = func(ka *IPFSKeyAPI) error { ka.ipns = ipns; return nil }
	}
	return option
}
