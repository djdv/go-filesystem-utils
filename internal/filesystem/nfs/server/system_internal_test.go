package nfs

import "github.com/go-git/go-billy/v5"

var (
	_ billy.Basic   = (*server)(nil)
	_ billy.Symlink = (*server)(nil)
)
