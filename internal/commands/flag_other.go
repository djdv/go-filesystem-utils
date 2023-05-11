//go:build !unix

package commands

import "io/fs"

func getUmask() fs.FileMode { return 0 }
