package mount

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	ipfs "github.com/djdv/go-filesystem-utils/filesystem/ipfscore"
	"github.com/djdv/go-filesystem-utils/filesystem/keyfs"
	"github.com/djdv/go-filesystem-utils/filesystem/pinfs"
	httpapi "github.com/ipfs/go-ipfs-http-client"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	ipfsBinding struct {
		client  coreiface.CoreAPI
		systems fsidMap
	}

	maddrString = string
	ipfsMap     map[maddrString]*ipfsBinding
)

// TODO: mutex concerns on map access when called from 2 processes at once.
func (env *mounter) getIPFS(fsid filesystem.ID, ipfsMaddr multiaddr.Multiaddr) (fs.FS, error) {
	bindings := env.ipfsBindings
	if bindings == nil {
		bindings = make(ipfsMap)
		env.ipfsBindings = bindings
	}
	var (
		nodeMaddr = ipfsMaddr.String()
		binding   = bindings[nodeMaddr]
	)
	if binding == nil {
		core, err := ipfsClient(ipfsMaddr)
		if err != nil {
			return nil, err
		}
		binding = &ipfsBinding{client: core, systems: make(fsidMap)}
		bindings[nodeMaddr] = binding
	}

	fileSystem := binding.systems[fsid]
	if fileSystem == nil {
		ctx := env.Context
		switch fsid {
		case filesystem.IPFS,
			filesystem.IPNS:
			fileSystem = ipfs.NewInterface(ctx, binding.client, fsid)
		case filesystem.PinFS:
			fileSystem = pinfs.NewInterface(ctx, binding.client)
		case filesystem.KeyFS:
			fileSystem = keyfs.NewInterface(ctx, binding.client)
		default:
			return nil, fmt.Errorf("TODO: real msg - fsid \"%s\" not yet supported", fsid.String())
		}
		binding.systems[fsid] = fileSystem
	}
	return fileSystem, nil
}

func ipfsClient(apiMaddr multiaddr.Multiaddr) (coreiface.CoreAPI, error) {
	ctx, cancelFunc := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelFunc()
	resolvedMaddr, err := resolveMaddr(ctx, apiMaddr)
	if err != nil {
		return nil, err
	}

	// TODO: I think the upstream package needs a patch to handle this internally.
	// we'll hack around it for now. Investigate later.
	// (When trying to use a unix socket for the IPFS maddr
	// the client returned from httpapi.NewAPI will complain on requests - forgot to copy the error lol)
	network, dialHost, err := manet.DialArgs(resolvedMaddr)
	if err != nil {
		return nil, err
	}
	switch network {
	default:
		return httpapi.NewApi(resolvedMaddr)
	case "unix":
		// TODO: consider patching cmds-lib
		// we want to use the URL scheme "http+unix"
		// as-is, it prefixes the value to be parsed by pkg `url` as "http://http+unix://"
		var (
			clientHost = "http://file-system-socket" // TODO: const + needs real name/value
			netDialer  = new(net.Dialer)
		)
		return httpapi.NewURLApiWithClient(clientHost, &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return netDialer.DialContext(ctx, network, dialHost)
				},
			},
		})
	}
}
