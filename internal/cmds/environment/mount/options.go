package mount

import "github.com/multiformats/go-multiaddr"

type (
	Option   interface{ apply(*settings) }
	settings struct {
		ipfsAPI multiaddr.Multiaddr
	}

	ipfsOpt struct{ multiaddr.Multiaddr }
)

func parseOptions(options ...Option) settings {
	var set settings
	for _, opt := range options {
		opt.apply(&set)
	}
	return set
}

// TODO: docs - the IPFS API maddr
func WithIPFS(m multiaddr.Multiaddr) Option { return ipfsOpt{m} }
func (m ipfsOpt) apply(set *settings)       { set.ipfsAPI = m.Multiaddr }
