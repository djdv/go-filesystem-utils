package filesystem

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/multiformats/go-multiaddr"
)

// TODO: I'm not sure if it makes sense to put these in this pkg.
// They're kind of like extensions to file system paths, and the types describe file systems
// but maybe it should go into fscmds somewhere instead.

//go:generate stringer -type=API,ID -linecomment -output=filesystem_string.go
type (
	// API represents a particular host API (e.g. 9P, Fuse, et al.)
	API uint
	// ID represents a particular file system implementation (e.g. IPFS, IPNS, et al.)
	ID uint

	MountPoint interface {
		//Source() multiaddr.Multiaddr
		Target() multiaddr.Multiaddr
		io.Closer
	}

	Mounter interface {
		Mount(_ context.Context, target multiaddr.Multiaddr) (MountPoint, error)
	}
)

const (
	apiStart API = iota
	Fuse
	Plan9Protocol // 9P
	apiEnd
)

const (
	idStart ID = iota
	IPFS
	IPNS
	Files
	PinFS
	KeyFS
	MFS
	idEnd

	// Existing Multicodec standards:
	PathProtocol = 0x2f
)

func RegisterPathMultiaddr() error {
	return multiaddr.AddProtocol(multiaddr.Protocol{
		Name:  "path",
		Code:  PathProtocol,
		VCode: multiaddr.CodeToVarint(PathProtocol),
		Size:  multiaddr.LengthPrefixedVarSize,
		Path:  true,
		Transcoder: multiaddr.NewTranscoderFromFunctions(
			func(s string) ([]byte, error) { return []byte(s), nil },
			func(b []byte) (string, error) { return string(b), nil },
			nil),
	})
}

func StringToID(s string) (ID, error) {
	normalized := strings.ToLower(s)
	for i := idStart + 1; i != idEnd; i++ {
		var (
			id     ID = i
			strVal    = strings.ToLower(id.String())
		)
		if normalized == strVal {
			return id, nil
		}
	}
	return 0, fmt.Errorf("invalid ID name \"%s\"", s)
}

func StringToAPI(s string) (API, error) {
	normalized := strings.ToLower(s)
	for i := apiStart + 1; i != apiEnd; i++ {
		var (
			api    API = i
			strVal     = strings.ToLower(api.String())
		)
		if normalized == strVal {
			return api, nil
		}
	}
	return 0, fmt.Errorf("invalid API name \"%s\"", s)
}
