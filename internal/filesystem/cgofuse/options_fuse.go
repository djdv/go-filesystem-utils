//go:build !nofuse

package cgofuse

import "github.com/winfsp/cgofuse/fuse"

func (settings *fuseHostSettings) apply(fsh *fuse.FileSystemHost) {
	for _, pair := range []struct {
		setter func(bool)
		bool
	}{
		{setter: fsh.SetCapReaddirPlus, bool: settings.readdirPlus},
		{setter: fsh.SetCapCaseInsensitive, bool: settings.caseInsensitive},
		{setter: fsh.SetCapDeleteAccess, bool: settings.deleteAccess},
	} {
		pair.setter(pair.bool)
	}
}
