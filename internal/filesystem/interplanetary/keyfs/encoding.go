package keyfs

import (
	"encoding/json"
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/commands/chmod"
	intp "github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/internal"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/ipns"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
	multiaddrEnc "github.com/djdv/go-filesystem-utils/internal/multiaddr"
	"github.com/multiformats/go-multiaddr"
)

// FSMaker represents a set of marshalable values
// that can be used to construct an [FS] instance.
// Suitable for RPC, storage, etc.
type FSMaker struct {
	IPNS        *ipns.FSMaker          `json:"ipns"`
	APIMaddr    multiaddrEnc.Multiaddr `json:"apiMaddr"`
	APITimeout  time.Duration          `json:"apiTimeout"`
	Permissions fs.FileMode            `json:"permissions"`
	CacheExpiry time.Duration          `json:"cacheExpiry"`
	LinkLimit   uint                   `json:"linkLimit"`
}

// Valid attribute names of [FSMaker.ParseField].
const (
	APIAttribute         = "apiMaddr"
	APITimeoutAttribute  = "apiTimeout"
	PermissionsAttribute = "permissions"
	ExpiryAttribute      = "cacheExpiry"
	LinkLimitAttribute   = "linkLimit"
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
	const (
		required = 6
		maximum  = required + 1
	)
	options := make([]Option, required, maximum)
	options[0] = WithDagService(coreAPI.Dag())
	options[1] = WithNameService(coreAPI.Name())
	options[2] = WithAPITimeout(settings.APITimeout)
	options[3] = WithPermissions(settings.Permissions)
	options[4] = CacheKeysFor(settings.CacheExpiry)
	options[5] = WithLinkLimit(settings.LinkLimit)
	if ipns := settings.IPNS; ipns != nil {
		fsys, err := ipns.MakeFS()
		if err != nil {
			return nil, err
		}
		options = append(options, WithIPNS(fsys))
	}
	return New(coreAPI.Key(), options...)
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
	case ExpiryAttribute:
		var duration time.Duration
		if duration, err = time.ParseDuration(value); err == nil {
			settings.CacheExpiry = duration
		}
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
		const subsystem = "ipns."
		if err = settings.IPNS.ParseField(
			strings.TrimPrefix(attribute, subsystem),
			value,
		); err == nil {
			break
		}
		var fErr mountpoint.FieldError
		if errors.As(err, &fErr) {
			tried := fErr.Tried
			for i, attr := range tried {
				tried[i] = subsystem + attr
			}
			fErr.Tried = append(
				tried,
				APIAttribute, APITimeoutAttribute,
				PermissionsAttribute, ExpiryAttribute,
				LinkLimitAttribute,
			)
			err = fErr
		}
	}
	return err
}
