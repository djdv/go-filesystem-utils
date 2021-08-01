package pinfs

import "github.com/ipfs/go-ipfs/filesystem"

// TODO: error on root
func (pi *pinInterface) Open(path string, flags filesystem.IOFlags) (filesystem.File, error) {
	return pi.ipfs.Open(path, flags)
}
