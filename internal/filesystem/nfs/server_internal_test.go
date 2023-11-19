package nfs

import "github.com/go-git/go-billy/v5"

var (
	_ billy.Basic   = (*netFS)(nil)
	_ billy.Symlink = (*netFS)(nil)
	_ billy.File    = (*netFile)(nil)
)
