package pinfs

// TODO: generic "if root, return some const error" func
func (pi *pinInterface) Make(path string) error          { return pi.ipfs.Make(path) }
func (pi *pinInterface) MakeDirectory(path string) error { return pi.ipfs.MakeDirectory(path) }
func (pi *pinInterface) MakeLink(path, linkTarget string) error {
	return pi.ipfs.MakeLink(path, linkTarget)
}
