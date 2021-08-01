package pinfs

// TODO: root behaviours; should unpin on call
func (pi *pinInterface) Remove(path string) error          { return pi.ipfs.Remove(path) }
func (pi *pinInterface) RemoveLink(path string) error      { return pi.ipfs.RemoveLink(path) }
func (pi *pinInterface) RemoveDirectory(path string) error { return pi.ipfs.RemoveDirectory(path) }
