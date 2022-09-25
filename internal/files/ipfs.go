package files

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
	httpapi "github.com/ipfs/go-ipfs-http-client"
	"github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
	manet "github.com/multiformats/go-multiaddr/net"
)

type (
	ipfsMounter struct {
		File
		fsid   filesystem.ID
		dataMu sync.Locker
		ipfsDataBuffer
		*ipfsMountData
		mountpoint io.Closer
		mount      mountFunc // TODO: placeholder name.
	}
	ipfsMountData struct {
		ApiMaddr ipfsAPIMultiaddr
		Target   string
	}
	ipfsDataBuffer struct {
		write *bytes.Buffer
		read  *bytes.Reader
	}
)

func newIPFSMounter(fsid filesystem.ID, mountFn mountFunc, options ...IPFSOption) (p9.QID, *ipfsMounter, error) {
	var settings ipfsSettings
	if err := parseOptions(&settings, options...); err != nil {
		return p9.QID{}, nil, err
	}
	var (
		metadata       = settings.metadata
		withTimestamps = settings.withTimestamps
	)
	initMetadata(&metadata, p9.ModeRegular, withTimestamps)
	return *metadata.QID, &ipfsMounter{
		File: File{
			metadata: metadata,
			link:     settings.linkSettings,
		},
		fsid:          fsid,
		dataMu:        new(sync.Mutex),
		ipfsMountData: new(ipfsMountData),
		mount:         mountFn,
		// dataBufferMu: new(sync.Mutex),
		// dataBuffer:   new(bytes.Buffer),
	}, nil
}

/*
func withAPIMaddr(serverMaddr multiaddr.Multiaddr) ipfsTargetOption {
	return func(it *ipfsTarget) error {
		it.options.ApiMaddr.Multiaddr = serverMaddr
		return nil
	}
}

// TODO: this might become a shared+exported option
func withMountTarget(target string) ipfsTargetOption {
	return func(it *ipfsTarget) error {
		it.options.Target = target
		return nil
	}
}
*/

func (im *ipfsMounter) clone(withQID bool) ([]p9.QID, *ipfsMounter, error) {
	var (
		qids []p9.QID
		// TODO: audit; struct changed, what fields specifically need to be copied.
		newIt = &ipfsMounter{
			File:           im.File,
			ipfsDataBuffer: im.ipfsDataBuffer,
			ipfsMountData:  im.ipfsMountData,
			// data:       it.data,
			mount:      im.mount,
			mountpoint: im.mountpoint,
			dataMu:     im.dataMu,
			// dataBuffer: it.write,
		}
	)
	if withQID {
		qids = []p9.QID{*newIt.QID}
	}
	return qids, newIt, nil
}

func (im *ipfsMounter) Walk(names []string) ([]p9.QID, p9.File, error) {
	return walk[*ipfsMounter](im, names...)
}

func (im *ipfsMounter) Open(mode p9.OpenFlags) (p9.QID, ioUnit, error) {
	if im.fidOpened() {
		return p9.QID{}, noIOUnit, perrors.EBADF
	}
	im.openFlag = true
	return *im.QID, noIOUnit, nil
}

func (im *ipfsMounter) WriteAt(p []byte, offset int64) (int, error) {
	im.dataMu.Lock()
	defer im.dataMu.Unlock()
	writer := im.write
	if writer == nil {
		writer = new(bytes.Buffer)
		im.write = writer
	}
	if dLen := writer.Len(); offset != int64(dLen) {
		err := fmt.Errorf("only contiguous writes are currently supported")
		return -1, err
	}
	return writer.Write(p)
}

func (im *ipfsMounter) ReadAt(p []byte, offset int64) (int, error) {
	im.dataMu.Lock()
	defer im.dataMu.Unlock()
	reader := im.read
	if reader == nil {
		b, err := json.Marshal(im.ipfsMountData)
		if err != nil {
			return -1, err
		}
		reader = bytes.NewReader(b)
		im.read = reader
	}
	return reader.ReadAt(p, offset)
}

func (im *ipfsMounter) Close() error {
	im.dataMu.Lock()
	defer im.dataMu.Unlock()
	if writer := im.write; writer != nil &&
		writer.Len() != 0 {
		var (
			targetData = writer.Bytes()
			targetPtr  = im.ipfsMountData
			err        = json.Unmarshal(targetData, targetPtr)
		)
		if err != nil {
			return err
		}
		if reader := im.read; reader != nil { // TODO export to method [invalidateReader]/[updateReader] or whatever. Data changed, so we re-encode and reset the reader.
			b, err := json.Marshal(targetPtr)
			if err != nil {
				return err
			}
			im.read = bytes.NewReader(b)
		}
		fsid := im.fsid
		goFS, err := ipfsToGoFS(fsid, targetPtr.ApiMaddr.Multiaddr)
		if err != nil {
			return err
		}
		closer, err := im.mount(goFS, targetPtr.Target)
		if err != nil {
			return err
		}
		im.mountpoint = closer
		/*
			// TODO: structure; fsh should have Attach-like method.
			// Not passed back to its package function.
			fsh, err := cgofuse.GoToFuse(goFS)
			if err != nil {
				return err
			}
			cgofuse.AttachToHost(fsh.FileSystemHost, fsid, targetPtr.Target)
			it.mountpoint = fsh
		*/
		/*
			fuse := cgofuse.NewFuseInterface(goFS, )
			closer, err := cgofuse.AttachToHost(fuse, fsid, targetPtr.Target)
			it.mountpoint = fuse
		*/
	}
	return nil
}

func ipfsToGoFS(fsid filesystem.ID, ipfsMaddr multiaddr.Multiaddr) (fs.FS, error) {
	client, err := ipfsClient(ipfsMaddr)
	if err != nil {
		return nil, err
	}
	// TODO [de-dupe]: convert PinFS to fallthrough to IPFS if possible.
	// Both need a client+IPFS-FS.
	switch fsid { // TODO: add all cases
	case filesystem.IPFS,
		filesystem.IPNS:
		return filesystem.NewIPFS(client, fsid), nil
	case filesystem.IPFSPins:
		ipfs := filesystem.NewIPFS(client, filesystem.IPFS)
		return filesystem.NewPinFS(client.Pin(),
			filesystem.WithIPFS[filesystem.PinfsOption](ipfs),
		), nil
	case filesystem.IPFSKeys:
		ipns := filesystem.NewIPFS(client, filesystem.IPNS)
		return filesystem.NewKeyFS(client.Key(),
			filesystem.WithIPNS[filesystem.KeyfsOption](ipns),
		), nil
	default:
		return nil, fmt.Errorf("%s has no handler", fsid)
	}
}

/*
func mountFuseIPFS(ipfsMaddr multiaddr.Multiaddr, fsid filesystem.ID, target string) (io.Closer, error) {
	client, err := ipfsClient(ipfsMaddr)
	if err != nil {
		return nil, err
	}
	var goFS fs.FS
	// TODO [de-dupe]: convert PinFS to fallthrough to IPFS if possible.
	// Both need a client+IPFS-FS.
	switch fsid { // TODO: add all cases
	case filesystem.IPFS,
		filesystem.IPNS:
		goFS = filesystem.NewIPFS(client, fsid)
	case filesystem.IPFSPins:
		ipfs := filesystem.NewIPFS(client, filesystem.IPFS)
		goFS = filesystem.NewPinFS(client.Pin(),
			filesystem.WithIPFS[filesystem.PinfsOption](ipfs),
		)
	case filesystem.IPFSKeys:
		ipns := filesystem.NewIPFS(client, filesystem.IPNS)
		goFS = filesystem.NewKeyFS(client.Key(),
			filesystem.WithIPNS[filesystem.KeyfsOption](ipns),
		)
	default:
		return nil, errors.New("not supported yet")
	}

	// TODO: 1 interface per subsystem directory, not 1 per mountpoint
	// I.e. 1 for /fuse/ipfs, 1 for /fuse/ipns, etc. not /fuse/ipfs/m1, /fuse/ipfs/m2
	// const dbgLog = false // TODO: plumbing from options.
	// ^ Still needed but as funcopts
	fsi := cgofuse.NewFuseInterface(goFS)
	return cgofuse.AttachToHost(fsi, fsid, target)
}
*/

func ipfsClient(apiMaddr multiaddr.Multiaddr) (*httpapi.HttpApi, error) {
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
