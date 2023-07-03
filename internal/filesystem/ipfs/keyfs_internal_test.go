package ipfs

import (
	"context"
	"io/fs"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

var (
	_ fs.FS           = (*KeyFS)(nil)
	_ fs.StatFS       = (*KeyFS)(nil)
	_ filesystem.IDFS = (*KeyFS)(nil)
	_ fs.File         = (*keyDirectory)(nil)
	_ fs.ReadDirFile  = (*keyDirectory)(nil)
)

func TestKeyFS(t *testing.T) {
	t.Parallel()
	t.Run("Options", testKeyFSOptions)
}

func testKeyFSOptions(t *testing.T) {
	t.Parallel()
	// Compile time check.
	// Constructor must support these shared options.
	NewKeyFS(
		nil,
		WithContext[KeyFSOption](context.Background()),
		WithPermissions[KeyFSOption](0),
	)
}
