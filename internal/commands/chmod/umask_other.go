//go:build !unix

package chmod

import "io/fs"

func getUmask() fs.FileMode { return 0 }
