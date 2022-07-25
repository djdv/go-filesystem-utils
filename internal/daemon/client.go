package daemon

import (
	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// TODO: move this?
const ServiceMaddr = "/ip4/127.0.0.1/tcp/564"

func Connect(serverMaddr multiaddr.Multiaddr, options ...ClientOption) (*p9.Client, error) {
	var (
		settings    = parseClientOptions(options...)
		protocolLog = settings.protocolLogger
	)

	// clientLog.Printf("trying to connect to: %s", serverMaddr)
	conn, err := manet.Dial(serverMaddr)
	if err != nil {
		return nil, err
	}

	var clientOpts []p9.ClientOpt
	if protocolLog != nil {
		clientOpts = []p9.ClientOpt{
			p9.WithClientLogger(protocolLog),
		}
	}

	client, err := p9.NewClient(conn, clientOpts...)
	if err != nil {
		return nil, err
	}
	// clientLog.Printf("got client for: %s", conn.RemoteAddr())
	return client, nil
}
