package files

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
	httpapi "github.com/ipfs/go-ipfs-http-client"
	"github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
	manet "github.com/multiformats/go-multiaddr/net"
)

type ipfsFile struct {
	templatefs.NoopFile
	Parent p9.File
	Path   *atomic.Uint64
	*p9.Attr
	*p9.QID
	opened bool
}

type ipfsTarget struct {
	templatefs.NoopFile

	wBufMu sync.Locker  // TODO: no external hats, roll into method box.
	wBuf   bytes.Buffer // TODO: pointer to buf, init on open(W)?

	metadata
	parentFile p9.File
	mountpoint io.Closer
	options    struct { // TODO: json encoding methods to be called directly.
		ApiMaddr ipfsAPIMultiaddr
		Target   string
	}
	fsid   filesystem.ID
	opened bool
}

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

func createIPFSTarget(fsid filesystem.ID, name string, permissions p9.FileMode,
	parent p9.File, path *atomic.Uint64,
	uid p9.UID, gid p9.GID,
) (p9.QID, error) {
	var (
		mountFile, err = makeIPFSTarget(fsid,
			permissions, uid, gid, WithPath(path))
		qid = *mountFile.QID
	)
	if err != nil {
		return qid, err
	}
	return qid, parent.Link(mountFile, name)
}

func makeIPFSTarget(fsid filesystem.ID,
	permissions p9.FileMode, uid p9.UID, gid p9.GID,
	options ...MetaOption,
) (*ipfsTarget, error) {
	ipfsFile := &ipfsTarget{
		metadata: makeMetadata(p9.ModeRegular, options...),
		fsid:     fsid,
		wBufMu:   new(sync.Mutex),
	}
	const withServerTimes = true
	return ipfsFile, setAttr(ipfsFile, &p9.Attr{
		Mode: permissions,
		UID:  uid,
		GID:  gid,
		// TODO: sizeof file in json bytes.
		// Size: uint64(len(listener.Multiaddr().Bytes())),
	}, withServerTimes)
}

func (it *ipfsTarget) clone(withQID bool) ([]p9.QID, *ipfsTarget, error) {
	var (
		qids  []p9.QID
		newIt = &ipfsTarget{
			metadata:   it.metadata,
			parentFile: it.parentFile,
			options:    it.options,
			mountpoint: it.mountpoint,
			wBufMu:     it.wBufMu,
			wBuf:       it.wBuf,
		}
	)
	if withQID {
		qids = []p9.QID{*newIt.QID}
	}
	return qids, newIt, nil
}

func (it *ipfsTarget) fidOpened() bool { return it.opened }
func (it *ipfsTarget) Walk(names []string) ([]p9.QID, p9.File, error) {
	return walk[*ipfsTarget](it, names...)
}

func (it *ipfsTarget) SetAttr(valid p9.SetAttrMask, attr p9.SetAttr) error {
	return it.metadata.SetAttr(valid, attr)
}

func (it *ipfsTarget) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return it.metadata.GetAttr(req)
}

func (it *ipfsTarget) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	if it.opened {
		return p9.QID{}, 0, perrors.EBADF
	}
	it.opened = true
	return *it.QID, 0, nil
}

func (it *ipfsTarget) WriteAt(p []byte, offset int64) (int, error) {
	it.wBufMu.Lock()
	defer it.wBufMu.Unlock()
	// FIXME: ignoring offset / assuming contiguous write calls.
	return it.wBuf.Write(p)
}

func (it *ipfsTarget) ReadAt(p []byte, offset int64) (int, error) {
	// TODO: Suboptimal; cost of construction and encoding.
	// Encode on change, store results. Same with reader, on Open().
	b, err := json.Marshal(it.options)
	if err != nil {
		return -1, err
	}
	return bytes.NewReader(b).ReadAt(p, offset)
}

func (it *ipfsTarget) Close() error {
	it.wBufMu.Lock()
	defer it.wBufMu.Unlock()
	// FIXME: Walk will trigger this too
	// We need to only clear the flag from the thread that opened it.
	// How? it is shared currently? not all fields? not when cloned for walk?
	it.opened = false

	// FIXME: wBuf shared with all, same problem as above.
	if wBuf := &it.wBuf; wBuf.Len() != 0 {
		defer wBuf.Reset()
		targetData := wBuf.Bytes()
		targetPtr := &it.options

		// log.Println("syncing:", string(targetData))
		// it.options.ApiMaddr.UnmarshalText
		// return json.Unmarshal(targetData, targetPtr)
		err := json.Unmarshal(targetData, targetPtr)
		if err != nil {
			return err
		}
		// log.Println("trying to mount:", it.fsid.String(), targetPtr.Target)
		closer, err := mountFuseIPFS(targetPtr.ApiMaddr.Multiaddr, it.fsid, targetPtr.Target)
		if err != nil {
			return err
		}
		it.mountpoint = closer
	}
	return nil
}

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
		goFS = filesystem.NewPinFS(client.Pin(), filesystem.WithIPFS(ipfs))
	case filesystem.IPFSKeys:
		goFS = filesystem.NewDBGFS() // FIXME: dbg fs for testing - remove.
	default:
		return nil, errors.New("not supported yet")
	}

	// TODO: 1 interface per subsystem directory, not 1 per mountpoint
	// I.e. 1 for /fuse/ipfs, 1 for /fuse/ipns, etc. not /fuse/ipfs/m1, /fuse/ipfs/m2
	// const dbgLog = false // TODO: plumbing from options.
	// ^ Still needed but as funcopts
	fsi, err := cgofuse.NewFuseInterface(goFS)
	if err != nil {
		return nil, err
	}
	return cgofuse.AttachToHost(fsi, fsid, target)
}

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
