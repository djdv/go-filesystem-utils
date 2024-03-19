package cgofuse_test

import (
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/mountpoint"
)

var (
	_ mountpoint.Mounter     = (*cgofuse.Mounter)(nil)
	_ mountpoint.FieldParser = (*cgofuse.Mounter)(nil)
)
