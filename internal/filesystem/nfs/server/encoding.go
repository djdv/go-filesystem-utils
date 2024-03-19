package nfs

import (
	"encoding/json"
	"io"
	"io/fs"
	"strconv"

	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
	multiaddrEnc "github.com/djdv/go-filesystem-utils/internal/multiaddr"
	"github.com/multiformats/go-multiaddr"
)

// Mounter represents a set of marshalable values
// that can be used to mount an [host] instance.
// Suitable for RPC, storage, etc.
type Mounter struct {
	Maddr      multiaddrEnc.Multiaddr `json:"maddr"`
	CacheLimit int                    `json:"cacheLimit"`
}

// Valid attribute names of [FSMaker.ParseField].
const (
	MaddrAttribute      = "maddr"
	CacheLimitAttribute = "cacheLimit"
)

func (settings *Mounter) Mount(fsys fs.FS) (io.Closer, error) {
	return Mount(
		settings.Maddr.Multiaddr,
		fsys,
		WithCacheLimit(settings.CacheLimit),
	)
}

func (settings *Mounter) MarshalJSON() ([]byte, error) {
	type shadow Mounter
	return json.Marshal((*shadow)(settings))
}

func (settings *Mounter) UnmarshalJSON(b []byte) error {
	type shadow Mounter
	return json.Unmarshal(b, (*shadow)(settings))
}

func (settings *Mounter) ParseField(attribute, value string) error {
	var err error
	switch attribute {
	case MaddrAttribute:
		var maddr multiaddr.Multiaddr
		if maddr, err = multiaddr.NewMultiaddr(value); err == nil {
			settings.Maddr = multiaddrEnc.Multiaddr{Multiaddr: maddr}
		}
	case CacheLimitAttribute:
		var limit int
		if limit, err = strconv.Atoi(value); err == nil {
			settings.CacheLimit = int(limit)
		}
	default:
		err = mountpoint.FieldError{
			Attribute: attribute,
			Tried: []string{
				MaddrAttribute, CacheLimitAttribute,
			},
		}
	}
	return err
}
