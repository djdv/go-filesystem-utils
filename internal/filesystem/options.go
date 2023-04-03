package filesystem

type (
	IPFSOption interface { // TODO: might not need this
		PinfsOption | KeyfsOption
	}

	PinfsOption func(*IPFSPinAPI) error
	KeyfsOption func(*IPFSKeyAPI) error
)

func WithIPFS[OT IPFSOption](ipfs OpenDirFS) (option OT) {
	switch fnPtrPtr := any(&option).(type) {
	case *PinfsOption:
		*fnPtrPtr = func(pa *IPFSPinAPI) error { pa.ipfs = ipfs; return nil }
	}
	return option
}

func WithIPNS[OT IPFSOption](ipns OpenDirFS) (option OT) {
	switch fnPtrPtr := any(&option).(type) {
	case *KeyfsOption:
		*fnPtrPtr = func(ka *IPFSKeyAPI) error { ka.ipns = ipns; return nil }
	}
	return option
}
