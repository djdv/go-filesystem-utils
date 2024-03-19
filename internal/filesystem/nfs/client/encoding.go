package nfs

import (
	"encoding/json"
	"io/fs"
	"strconv"

	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
	multiaddrEnc "github.com/djdv/go-filesystem-utils/internal/multiaddr"
	"github.com/multiformats/go-multiaddr"
)

// FSMaker represents a set of marshalable values
// that can be used to construct an [FS] instance.
// Suitable for RPC, storage, etc.
type FSMaker struct {
	Maddr         multiaddrEnc.Multiaddr `json:"maddr"`
	UID           *uint32                `json:"uid"`
	GID           *uint32                `json:"gid"`
	Hostname      string                 `json:"hostname"`
	Dirpath       string                 `json:"dirpath"`
	LinkSeparator string                 `json:"linkSeparator"`
	LinkLimit     uint                   `json:"linkLimit"`
}

// Valid attribute names of [FSMaker.ParseField].
const (
	MaddrAttribute         = "maddr"
	UIDAttribute           = "uid"
	GIDAttribute           = "gid"
	HostnameAttribute      = "hostname"
	DirpathAttribute       = "dirpath"
	LinkSeparatorAttribute = "linkSeparator"
	LinkLimitAttribute     = "linkLimit"
)

func (settings *FSMaker) MakeFS() (fs.FS, error) {
	const (
		required = 4
		maximum  = required + 2
	)
	options := make([]Option, required, maximum)
	options[0] = WithHostname(settings.Hostname)
	options[1] = WithDirpath(settings.Dirpath)
	options[2] = WithLinkSeparator(settings.LinkSeparator)
	options[3] = WithLinkLimit(settings.LinkLimit)
	if uid := settings.UID; uid != nil {
		options = append(options, WithUID(*uid))
	}
	if gid := settings.GID; gid != nil {
		options = append(options, WithGID(*gid))
	}
	return New(settings.Maddr.Multiaddr, options...)
}

func (settings *FSMaker) MarshalJSON() ([]byte, error) {
	type shadow FSMaker
	return json.Marshal((*shadow)(settings))
}

func (settings *FSMaker) UnmarshalJSON(data []byte) error {
	type shadow FSMaker
	return json.Unmarshal(data, (*shadow)(settings))
}

func (settings *FSMaker) ParseField(attribute, value string) error {
	var err error
	switch attribute {
	case MaddrAttribute:
		var maddr multiaddr.Multiaddr
		if maddr, err = multiaddr.NewMultiaddr(value); err == nil {
			settings.Maddr = multiaddrEnc.Multiaddr{Multiaddr: maddr}
		}
	case HostnameAttribute:
		settings.Hostname = value
	case DirpathAttribute:
		settings.Dirpath = value
	case LinkSeparatorAttribute:
		settings.LinkSeparator = value
	case LinkLimitAttribute:
		var limit uint
		if limit, err = parseUint(value); err == nil {
			settings.LinkLimit = limit
		}
	case UIDAttribute:
		var uid uint32
		if uid, err = parseID(value); err == nil {
			settings.UID = &uid
		}
	case GIDAttribute:
		var gid uint32
		if gid, err = parseID(value); err == nil {
			settings.GID = &gid
		}
	default:
		err = mountpoint.FieldError{
			Attribute: attribute,
			Tried: []string{
				MaddrAttribute, HostnameAttribute,
				DirpathAttribute, LinkSeparatorAttribute,
				LinkLimitAttribute, UIDAttribute,
				GIDAttribute,
			},
		}
	}
	return err
}

func parseUint(value string) (uint, error) {
	const (
		base = 10
		size = 0
	)
	integer, err := strconv.ParseUint(value, base, size)
	return uint(integer), err
}

func parseID(value string) (uint32, error) {
	const (
		base = 10
		size = 32
	)
	actual, err := strconv.ParseUint(value, 0, 32)
	return uint32(actual), err
}
