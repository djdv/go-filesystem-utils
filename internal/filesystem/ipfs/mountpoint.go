package ipfs

import (
	"encoding/json"
	"errors"
	"io/fs"
	"strconv"
	"time"

	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	"github.com/multiformats/go-multiaddr"
)

type (
	IPFSGuest struct {
		APIMaddr            multiaddr.Multiaddr `json:"apiMaddr,omitempty"`
		APITimeout          time.Duration       `json:"apiTimeout,omitempty"`
		NodeCacheCount      int                 `json:"nodeCacheCount,omitempty"`
		DirectoryCacheCount int                 `json:"directoryCacheCount,omitempty"`
	}
	IPNSGuest struct {
		IPFSGuest
		NodeExpiry time.Duration `json:"cacheExpiry,omitempty"`
	}
	PinFSGuest struct {
		IPFSGuest
		CacheExpiry time.Duration `json:"cacheExpiry,omitempty"`
	}
	KeyFSGuest struct{ IPNSGuest }
)

func (ig *IPFSGuest) UnmarshalJSON(b []byte) error {
	// multiformats/go-multiaddr issue #100
	var maddrWorkaround struct {
		APIMaddr multiaddrContainer `json:"apiMaddr,omitempty"`
	}
	if err := json.Unmarshal(b, &maddrWorkaround); err != nil {
		return err
	}
	ig.APIMaddr = maddrWorkaround.APIMaddr.Multiaddr
	return json.Unmarshal(b, &struct {
		APITimeout          *time.Duration `json:"apiTimeout,omitempty"`
		NodeCacheCount      *int           `json:"nodeCacheCount,omitempty"`
		DirectoryCacheCount *int           `json:"directoryCacheCount,omitempty"`
	}{
		APITimeout:          &ig.APITimeout,
		NodeCacheCount:      &ig.NodeCacheCount,
		DirectoryCacheCount: &ig.DirectoryCacheCount,
	})
}

func (ig *IPFSGuest) ParseField(key, value string) error {
	const (
		apiKey            = "apiMaddr"
		apiTimeoutKey     = "apiTimeout"
		nodeCacheKey      = "nodeCacheCount"
		directoryCacheKey = "directoryCacheCount"
	)
	var err error
	switch key {
	case apiKey:
		var maddr multiaddr.Multiaddr
		if maddr, err = multiaddr.NewMultiaddr(value); err == nil {
			ig.APIMaddr = maddr
		}
	case apiTimeoutKey:
		var timeout time.Duration
		if timeout, err = time.ParseDuration(value); err == nil {
			ig.APITimeout = timeout
		}
	case nodeCacheKey:
		err = ig.parseCacheField(value, &ig.NodeCacheCount)
	case directoryCacheKey:
		err = ig.parseCacheField(value, &ig.DirectoryCacheCount)
	default:
		return p9fs.FieldError{
			Key: key,
			Tried: []string{
				apiKey, apiTimeoutKey,
				nodeCacheKey, directoryCacheKey,
			},
		}
	}
	return err
}

func (ig *IPFSGuest) parseCacheField(value string, target *int) error {
	i, err := strconv.ParseInt(value, 0, 64)
	if err != nil {
		return err
	}
	// HACK: [MakeFS] can't tell the difference
	// between uninitialized 0 and explicit 0.
	// Currently, negative values and 0 both disable the cache.
	// So hijack user input and replace with -1.
	if i == 0 {
		i--
	}
	*target = int(i)
	return nil
}

func (ig *IPFSGuest) makeCoreAPI() (coreiface.CoreAPI, error) {
	return newIPFSClient(ig.APIMaddr)
}

func (ig *IPFSGuest) MakeFS() (fs.FS, error) {
	client, err := ig.makeCoreAPI()
	if err != nil {
		return nil, err
	}
	return ig.makeFS(client)
}

func (ig *IPFSGuest) makeFS(api coreiface.CoreAPI) (fs.FS, error) {
	var options []IPFSOption
	if count := ig.NodeCacheCount; count != 0 {
		options = append(options, WithNodeCacheCount(count))
	}
	if count := ig.DirectoryCacheCount; count != 0 {
		options = append(options, WithDirectoryCacheCount(count))
	}
	return NewIPFS(api, options...)
}

func (ng *IPNSGuest) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &ng.IPFSGuest); err != nil {
		return err
	}
	return json.Unmarshal(b, &struct {
		NodeExpiry *time.Duration `json:"cacheExpiry,omitempty"`
	}{
		NodeExpiry: &ng.NodeExpiry,
	})
}

func (ng *IPNSGuest) ParseField(key, value string) error {
	const cacheKey = "cacheExpiry"
	switch key {
	case cacheKey:
		duration, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		ng.NodeExpiry = duration
		return nil
	default:
		if err := ng.IPFSGuest.ParseField(key, value); err != nil {
			var fErr p9fs.FieldError
			if errors.As(err, &fErr) {
				fErr.Tried = append(fErr.Tried, cacheKey)
				return fErr
			}
			return err
		}
		return nil
	}
}

func (ng *IPNSGuest) MakeFS() (fs.FS, error) {
	client, err := ng.makeCoreAPI()
	if err != nil {
		return nil, err
	}
	ipfs, err := ng.IPFSGuest.makeFS(client)
	if err != nil {
		return nil, err
	}
	var options []IPNSOption
	if expiry := ng.NodeExpiry; expiry != 0 {
		options = []IPNSOption{CacheNodesFor(expiry)}
	}
	return NewIPNS(client, ipfs, options...)
}

func (pg *PinFSGuest) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &pg.IPFSGuest); err != nil {
		return err
	}
	return json.Unmarshal(b, &struct {
		CacheExpiry *time.Duration `json:"cacheExpiry,omitempty"`
	}{
		CacheExpiry: &pg.CacheExpiry,
	})
}

func (pg *PinFSGuest) MakeFS() (fs.FS, error) {
	client, err := pg.makeCoreAPI()
	if err != nil {
		return nil, err
	}
	ipfsFS, err := pg.IPFSGuest.makeFS(client)
	if err != nil {
		return nil, err
	}
	return NewPinFS(
		client.Pin(),
		WithIPFS(ipfsFS),
		CachePinsFor(pg.CacheExpiry),
	)
}

func (pg *PinFSGuest) ParseField(key, value string) error {
	const cacheKey = "cacheExpiry"
	switch key {
	case cacheKey:
		duration, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		pg.CacheExpiry = duration
		return nil
	default:
		if err := pg.IPFSGuest.ParseField(key, value); err != nil {
			var fErr p9fs.FieldError
			if errors.As(err, &fErr) {
				fErr.Tried = append(fErr.Tried, cacheKey)
				return fErr
			}
			return err
		}
		return nil
	}
}

func (kg *KeyFSGuest) MakeFS() (fs.FS, error) {
	client, err := kg.makeCoreAPI()
	if err != nil {
		return nil, err
	}
	ipfs, err := kg.IPFSGuest.makeFS(client)
	if err != nil {
		return nil, err
	}
	// TODO: options
	ipnsFS, err := NewIPNS(client, ipfs)
	if err != nil {
		return nil, err
	}
	return NewKeyFS(client.Key(),
		WithIPNS(ipnsFS),
	)
}
