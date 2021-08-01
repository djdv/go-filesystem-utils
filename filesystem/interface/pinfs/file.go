package pinfs

import "github.com/djdv/go-filesystem-utils/filesystem"

// TODO: error on root
func (pi *pinInterface) Open(path string, flags filesystem.IOFlags) (filesystem.File, error) {
	return pi.ipfs.Open(path, flags)
}
