package daemon

import (
	"fmt"

	"github.com/hugelgupf/p9/p9"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/u-root/uio/ulog"
)

type Client struct {
	p9Client *p9.Client
	log      ulog.Logger
}

func Connect(serverMaddr multiaddr.Multiaddr, options ...ClientOption) (*Client, error) {
	client := new(Client)
	for _, setFunc := range options {
		if err := setFunc(client); err != nil {
			panic(err)
		}
	}

	conn, err := manet.Dial(serverMaddr)
	if err != nil {
		return nil, err
	}

	var clientOpts []p9.ClientOpt
	if clientLog := client.log; clientLog != nil {
		clientOpts = []p9.ClientOpt{
			p9.WithClientLogger(clientLog),
		}
	}

	p9Client, err := p9.NewClient(conn, clientOpts...)
	if err != nil {
		return nil, err
	}
	client.p9Client = p9Client
	return client, nil
}

func (c *Client) Shutdown() error {
	if c.p9Client == nil {
		// TODO: better message; maybe better logic?
		// Can we prevent this from being possible without unexporting [Client]?
		return fmt.Errorf("client is not connected")
	}
	// TODO: Find our socket within c.shutdownNames (settable via options)
	// and do the shutdown dance.
	return fmt.Errorf("not implemented yet")
}
