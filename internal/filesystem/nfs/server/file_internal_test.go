package nfs

import "github.com/go-git/go-billy/v5"

var _ billy.File = (*file)(nil)
