package interplanetary

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/generic"
	"github.com/ipfs/kubo/client/rpc"
	"github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
	manet "github.com/multiformats/go-multiaddr/net"
)

const errCantResolveAPI = generic.ConstError("non-resolvable API endpoint")

func NewIPFSClient(apiMaddr multiaddr.Multiaddr) (*rpc.HttpApi, error) {
	address, client, err := newHTTPClient(apiMaddr)
	if err != nil {
		return nil, err
	}
	return rpc.NewURLApiWithClient(address, client)
}

func newHTTPClient(apiMaddr multiaddr.Multiaddr) (string, *http.Client, error) {
	const timeout = 30 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	resolvedMaddr, err := resolveMaddr(ctx, apiMaddr)
	if err != nil {
		return "", nil, err
	}

	network, address, err := manet.DialArgs(resolvedMaddr)
	if err != nil {
		return "", nil, err
	}
	var client *http.Client
	switch network {
	case "unix":
		// NOTE: [go-ipfs-http-client] would need to patch [httpaoi.NewApi]
		// to handle this internally.
		// Without a custom `http.Client`, `httpapi.HttpApi` fails when
		// making requests to unix domain sockets.
		address, client = udsHTTPClient(address)
	default:
		client = &http.Client{
			Transport: &http.Transport{
				Proxy:             http.ProxyFromEnvironment,
				DisableKeepAlives: true,
			},
		}
	}
	return address, client, nil
}

func udsHTTPClient(address string) (string, *http.Client) {
	var (
		// NOTE: `http+unix` scheme is not supported in Go (1.20)
		// udsUrl = "http+unix://" + url.PathEscape(address)
		// BUG: [httpapi.NewRequest] always prepends `http://`
		// if prefix is not `http`; which would mangle our url anyway.
		fakeAddress = "http://unix-domain-socket"
		netDialer   = new(net.Dialer)
		client      = &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return netDialer.DialContext(ctx, "unix", address)
				},
			},
		}
	)
	return fakeAddress, client
}

func resolveMaddr(ctx context.Context, addr multiaddr.Multiaddr) (multiaddr.Multiaddr, error) {
	addrs, err := madns.DefaultResolver.Resolve(ctx, addr)
	if err != nil {
		return nil, err
	}

	if len(addrs) == 0 {
		return nil, errCantResolveAPI
	}

	return addrs[0], nil
}
