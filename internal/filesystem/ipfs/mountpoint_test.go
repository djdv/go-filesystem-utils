package ipfs_test

import (
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/ipfs"
)

var (
	_ p9fs.GuestIdentifier = (*ipfs.IPFSGuest)(nil)
	_ p9fs.GuestIdentifier = (*ipfs.PinFSGuest)(nil)
	_ p9fs.GuestIdentifier = (*ipfs.IPNSGuest)(nil)
	_ p9fs.GuestIdentifier = (*ipfs.KeyFSGuest)(nil)
)
