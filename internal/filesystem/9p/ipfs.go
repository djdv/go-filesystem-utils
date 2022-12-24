package p9

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sync"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/ipfs"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
	"github.com/multiformats/go-multiaddr"
)

type (
	ipfsMounter struct {
		File
		fsid   filesystem.ID
		dataMu sync.Locker
		ipfsDataBuffer
		*ipfsMountData
		// TODO: unmount should have its own mutex, and probably abstraction.
		unmount *detachFunc // NOTE: Shared R/W access across all FIDs.
		mountFn MountFunc
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

func newIPFSMounter(fsid filesystem.ID, mountFn MountFunc, options ...IPFSOption) (p9.QID, *ipfsMounter, error) {
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
		mountFn:       mountFn,
		unmount:       new(detachFunc),
	}, nil
}

func (im *ipfsMounter) clone(withQID bool) ([]p9.QID, *ipfsMounter, error) {
	var (
		qids []p9.QID
		// TODO: audit; struct changed, what fields specifically need to be copied.
		newIt = &ipfsMounter{
			// File:           im.File,
			File: File{
				metadata: im.File.metadata,
				link:     im.File.link,
			},
			fsid: im.fsid,
			// TODO: can we wrap this up into a (general) type? *bufferedWriterSync?
			dataMu:         im.dataMu,
			ipfsDataBuffer: im.ipfsDataBuffer,
			ipfsMountData:  im.ipfsMountData,
			//
			mountFn: im.mountFn,
			unmount: im.unmount,
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
	im.File.openFlag = true
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
		defer writer.Reset() // TODO: review where this should happen.
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
		// FIXME: ping IPFS node here. If it's not alive, don't even try to mount it.
		// ^ Don't do this; file system calls should not depend on connection state
		// (system-wide, per-call may error, but not total failure).
		closer, err := im.mountFn(goFS, targetPtr.Target)
		if err != nil {
			// TODO: We do this for now in case the CLI call fails
			// but should handle this differently for API callers.
			// Add some flag like `unlink-on-failure` or something.
			// (The reason for this is so the background process doesn't hang around forever
			// thinking it has an active mountpoint when it doesn't)
			if parent := im.File.link.parent; parent != nil {
				// TODO: We'll need to handle the error too.
				parent.UnlinkAt(im.File.link.name, 0)
			}
			// TODO: error format
			return fmt.Errorf("%w: %s", perrors.EIO, err)
		}
		*im.unmount = closer.Close
	}
	return nil
}

func (im *ipfsMounter) Detach() error {
	detach := *im.unmount
	if detach == nil {
		return errors.New("not attached") // TODO: error message+value
	}
	return detach()
}

func ipfsToGoFS(fsid filesystem.ID, ipfsMaddr multiaddr.Multiaddr) (fs.FS, error) {
	client, err := ipfsClient(ipfsMaddr)
	if err != nil {
		return nil, err
	}
	// TODO [de-dupe]: convert PinFS to fallthrough to IPFS if possible.
	// Both need a client+IPFS-FS.
	switch fsid { // TODO: add all cases
	case filesystem.IPFS, filesystem.IPNS:
		return ipfs.NewIPFS(client, fsid), nil
	case filesystem.IPFSPins:
		ipfsFS := ipfs.NewIPFS(client, filesystem.IPFS)
		return ipfs.NewPinFS(client.Pin(),
			ipfs.WithIPFS[ipfs.PinfsOption](ipfsFS),
		), nil
	case filesystem.IPFSKeys:
		ipnsFS := ipfs.NewIPFS(client, filesystem.IPNS)
		return ipfs.NewKeyFS(client.Key(),
			ipfs.WithIPNS[ipfs.KeyfsOption](ipnsFS),
		), nil
	case filesystem.MFS:
		return getMFSMountRoot(ipfsMaddr)
	default:
		return nil, fmt.Errorf("%s has no handler", fsid)
	}
}
