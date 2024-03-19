//go:build !noipfs

package daemon

import (
	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/ipfs"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/ipns"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/keyfs"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/interplanetary/pinfs"
)

func makeIPFSGuests[
	hostI hostPtr[host],
	host any,
](hostID filesystem.Host, guests mountPointGuests, path ninePath,
) {
	guests[ipfs.ID] = newMountPointFunc[hostI, ipfs.FSMaker](hostID, ipfs.ID, path)
	guests[pinfs.ID] = newMountPointFunc[hostI, pinfs.FSMaker](hostID, pinfs.ID, path)
	guests[ipns.ID] = newMountPointFunc[hostI, ipns.FSMaker](hostID, ipns.ID, path)
	guests[keyfs.ID] = newMountPointFunc[hostI, keyfs.FSMaker](hostID, keyfs.ID, path)
}
