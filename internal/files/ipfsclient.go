package files

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	oldapi "github.com/ipfs/go-ipfs-api"
	httpapi "github.com/ipfs/go-ipfs-http-client"
	"github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
	manet "github.com/multiformats/go-multiaddr/net"
)

func ipfsClient(apiMaddr multiaddr.Multiaddr) (*httpapi.HttpApi, error) {
	address, client, err := prepareClientTransport(apiMaddr)
	if err != nil {
		return nil, err
	}
	return httpapi.NewURLApiWithClient(address, client)
}

func ipfsClient_old(apiMaddr multiaddr.Multiaddr) (*oldapi.Shell, error) {
	address, client, err := prepareClientTransport(apiMaddr)
	if err != nil {
		return nil, err
	}
	return oldapi.NewShellWithClient(address, client), nil
}

func prepareClientTransport(apiMaddr multiaddr.Multiaddr) (string, *http.Client, error) {
	// TODO: magic number; decide on good timeout and const it.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resolvedMaddr, err := resolveMaddr(ctx, apiMaddr)
	if err != nil {
		return "", nil, err
	}

	// TODO: I think the upstream package needs a patch to handle this internally.
	// we'll hack around it for now. Investigate later.
	// (When trying to use a unix socket for the IPFS maddr
	// the client returned from httpapi.NewAPI will complain on requests - forgot to copy the error lol)
	network, address, err := manet.DialArgs(resolvedMaddr)
	if err != nil {
		return "", nil, err
	}
	switch network {
	default:
		client := &http.Client{
			Transport: &http.Transport{
				Proxy:             http.ProxyFromEnvironment,
				DisableKeepAlives: true,
			},
		}
		return address, client, nil
	case "unix":
		address, client := udsHTTPClient(network, address)
		return address, client, nil
	}
}

func udsHTTPClient(network, address string) (string, *http.Client) {
	// TODO: consider patching cmds-lib
	// we want to use the URL scheme "http+unix"
	// as-is, it prefixes the value to be parsed by pkg `url` as "http://http+unix://"
	var (
		fakeAddress = "http://file-system-socket" // TODO: const + needs real name/value
		netDialer   = new(net.Dialer)
		client      = &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return netDialer.DialContext(ctx, network, address)
				},
			},
		}
	)
	return fakeAddress, client
}

func resolveMaddr(ctx context.Context, addr multiaddr.Multiaddr) (multiaddr.Multiaddr, error) {
	ctx, cancelFunc := context.WithTimeout(ctx, 10*time.Second)
	defer cancelFunc()

	addrs, err := madns.DefaultResolver.Resolve(ctx, addr)
	if err != nil {
		return nil, err
	}

	if len(addrs) == 0 {
		return nil, errors.New("non-resolvable API endpoint")
	}

	return addrs[0], nil
}
