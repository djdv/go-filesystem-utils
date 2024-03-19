package ipfs

import (
	"encoding/json"
	"io/fs"
	"strconv"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/commands/chmod"
	intp "github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/internal"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
	multiaddrEnc "github.com/djdv/go-filesystem-utils/internal/multiaddr"
	"github.com/multiformats/go-multiaddr"
)

// FSMaker represents a set of marshalable values
// that can be used to construct an [FS] instance.
// Suitable for RPC, storage, etc.
type FSMaker struct {
	APIMaddr            multiaddrEnc.Multiaddr `json:"apiMaddr"`
	APITimeout          time.Duration          `json:"apiTimeout"`
	Permissions         fs.FileMode            `json:"permissions"`
	NodeCacheCount      int                    `json:"nodeCacheCount"`
	DirectoryCacheCount int                    `json:"directoryCacheCount"`
	LinkLimit           uint                   `json:"linkLimit"`
}

// Valid attribute names of [FSMaker.ParseField].
const (
	APIAttribute            = "apiMaddr"
	APITimeoutAttribute     = "apiTimeout"
	PermissionsAttribute    = "permissions"
	NodeCacheAttribute      = "nodeCacheCount"
	DirectoryCacheAttribute = "directoryCacheCount"
	LinkLimitAttribute      = "linkLimit"
)

func (settings *FSMaker) MakeFS() (fs.FS, error) {
	maddr := settings.APIMaddr.Multiaddr
	if maddr == nil {
		maddrs, err := intp.IPFSAPIs()
		if err != nil {
			return nil, err
		}
		maddr = maddrs[0]
	}
	coreAPI, err := intp.NewIPFSClient(maddr)
	if err != nil {
		return nil, err
	}
	return New(
		coreAPI,
		WithAPITimeout(settings.APITimeout),
		WithNodeCacheCount(settings.NodeCacheCount),
		WithDirectoryCacheCount(settings.DirectoryCacheCount),
		WithLinkLimit(settings.LinkLimit),
		WithPermissions(settings.Permissions),
	)
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
	case APIAttribute:
		var maddr multiaddr.Multiaddr
		if maddr, err = multiaddr.NewMultiaddr(value); err == nil {
			settings.APIMaddr = multiaddrEnc.Multiaddr{Multiaddr: maddr}
		}
	case APITimeoutAttribute:
		var timeout time.Duration
		if timeout, err = time.ParseDuration(value); err == nil {
			settings.APITimeout = timeout
		}
	case PermissionsAttribute:
		var modified fs.FileMode
		if modified, err = chmod.ParsePermissions(settings.Permissions, value); err == nil {
			err = intp.SetModePermissions(&settings.Permissions, modified)
		}
	case NodeCacheAttribute:
		err = settings.parseCacheField(value, &settings.NodeCacheCount)
	case DirectoryCacheAttribute:
		err = settings.parseCacheField(value, &settings.DirectoryCacheCount)
	case LinkLimitAttribute:
		const (
			base = 10
			size = 0
		)
		var limit uint64
		if limit, err = strconv.ParseUint(value, base, size); err == nil {
			settings.LinkLimit = uint(limit)
		}
	default:
		err = mountpoint.FieldError{
			Attribute: attribute,
			Tried: []string{
				APIAttribute, APITimeoutAttribute,
				NodeCacheAttribute, DirectoryCacheAttribute,
				LinkLimitAttribute, PermissionsAttribute,
			},
		}
	}
	return err
}

func (settings *FSMaker) parseCacheField(value string, target *int) error {
	i, err := strconv.ParseInt(value, 0, 64)
	if err != nil {
		return err
	}
	// HACK: [MakeFS] can't tell the difference
	// between uninitialized 0 and explicit 0.
	// Currently, negative values and 0 both disable the cache.
	// So hijack user input and replace with -1.
	if i == 0 {
		i = -1
	}
	*target = int(i)
	return nil
}
