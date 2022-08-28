package filesystem

import "github.com/djdv/go-filesystem-utils/internal/generic"

// TODO: better names + docs?

//go:generate stringer -type=API,ID -linecomment -output=id_string.go
type (
	// API represents a particular host API
	// (9P, Fuse, et al.)
	API uint
	// ID represents a particular file system implementation
	// (IPFS, IPNS, et al.)
	ID uint
)

const (
	apiStart API = iota //
	Fuse
	Plan9Protocol // 9P
	apiEnd        //
)

const (
	idStart  ID = iota //
	IPFSPins           // PinFS
	IPFS
	IPFSKeys // KeyFS
	IPNS
	IPFSFiles // Files
	MFS
	idEnd //

	// Existing Multicodec standards:
	// PathProtocol = 0x2f
)

func ParseID(id string) (ID, error)    { return generic.ParseEnum(idStart, idEnd, id) }
func ParseAPI(api string) (API, error) { return generic.ParseEnum(apiStart, apiEnd, api) }
