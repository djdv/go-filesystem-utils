package ipfs

import (
	"context"
	"io"
	"io/fs"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

var (
	_ fs.FS           = (*IPNS)(nil)
	_ fs.StatFS       = (*IPNS)(nil)
	_ filesystem.IDFS = (*IPNS)(nil)
	_ fs.File         = (*ipnsFile)(nil)
	_ fs.ReadDirFile  = (*ipnsFile)(nil)
	_ io.Seeker       = (*ipnsFile)(nil)
)

func TestIPNS(t *testing.T) {
	t.Parallel()
	t.Run("Options", testIPNSOptions)
}

func testIPNSOptions(t *testing.T) {
	t.Parallel()
	// Compile time check.
	// Constructor must support these shared options.
	NewIPNS(
		nil, nil,
		WithContext[IPNSOption](context.Background()),
		WithPermissions[IPNSOption](0),
	)
}
