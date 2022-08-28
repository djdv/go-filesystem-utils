package filesystem

import (
	"io"
	"io/fs"
)

// TODO: name?
type FS interface {
	fs.FS
	// fs.StatFS
	// fs.ReadDirFS
	// fs.SubFS ?
	OpenDirFS
}

type File interface {
	fs.File
	io.Seeker
}

type OpenDirFS interface {
	fs.FS
	OpenDir(name string) (fs.ReadDirFile, error)
}

/*
type Directory interface {
	fs.ReadDirFile
}
*/
