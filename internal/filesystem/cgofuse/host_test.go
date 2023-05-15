package cgofuse_test

import (
	p9fs "github.com/djdv/go-filesystem-utils/internal/filesystem/9p"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
)

var (
	_ p9fs.Mounter        = (*cgofuse.Host)(nil)
	_ p9fs.HostIdentifier = (*cgofuse.Host)(nil)
)
