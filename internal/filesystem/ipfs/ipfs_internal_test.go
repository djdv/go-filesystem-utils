package ipfs

import (
	"context"
	"io/fs"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

var (
	_ fs.FS                    = (*IPFS)(nil)
	_ fs.StatFS                = (*IPFS)(nil)
	_ filesystem.IDFS          = (*IPFS)(nil)
	_ symlinkRFS               = (*IPFS)(nil)
	_ fs.File                  = (*ipfsDirectory)(nil)
	_ fs.ReadDirFile           = (*ipfsDirectory)(nil)
	_ filesystem.StreamDirFile = (*ipfsDirectory)(nil)
)

func TestIPFS(t *testing.T) {
	t.Parallel()
	t.Run("Options", testIPFSOptions)
}

func testIPFSOptions(t *testing.T) {
	t.Parallel()
	// Compile time check.
	// Constructor must support these shared options.
	NewIPFS(
		nil,
		WithContext[IPFSOption](context.Background()),
		WithPermissions[IPFSOption](0),
		WithNodeTimeout[IPFSOption](0),
		WithLinkLimit[IPFSOption](0),
	)
}
