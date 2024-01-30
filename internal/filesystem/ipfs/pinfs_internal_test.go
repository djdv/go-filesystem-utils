package ipfs

import (
	"context"
	"io/fs"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

var (
	_ fs.FS                    = (*PinFS)(nil)
	_ fs.StatFS                = (*PinFS)(nil)
	_ filesystem.IDFS          = (*PinFS)(nil)
	_ symlinkFS                = (*PinFS)(nil)
	_ fs.File                  = (*pinDirectory)(nil)
	_ fs.ReadDirFile           = (*pinDirectory)(nil)
	_ filesystem.StreamDirFile = (*pinDirectory)(nil)
)

func TestPinFS(t *testing.T) {
	t.Parallel()
	t.Run("Options", testPinFSOptions)
}

func testPinFSOptions(t *testing.T) {
	t.Parallel()
	// Compile time check.
	// Constructor must support these shared options.
	NewPinFS(
		nil,
		WithContext[PinFSOption](context.Background()),
		WithPermissions[PinFSOption](0),
		WithDagService[PinFSOption](nil),
	)
}
