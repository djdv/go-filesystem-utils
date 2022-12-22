package ipfs

import "io/fs"

type (
	IPFSOption interface { // TODO: might not need this
		PinfsOption | KeyfsOption
	}

	PinfsOption func(*IPFSPinFS) error
	KeyfsOption func(*IPFSKeyFS) error
)

func WithIPFS[OT IPFSOption](ipfs OpenDirFS) (option OT) {
	switch fnPtrPtr := any(&option).(type) {
	case *PinfsOption:
		*fnPtrPtr = func(pa *IPFSPinFS) error { pa.ipfs = ipfs; return nil }
	}
	return option
}

func WithIPNS[OT IPFSOption](ipns fs.FS) (option OT) {
	switch fnPtrPtr := any(&option).(type) {
	case *KeyfsOption:
		*fnPtrPtr = func(ka *IPFSKeyFS) error { ka.ipns = ipns; return nil }
	}
	return option
}
