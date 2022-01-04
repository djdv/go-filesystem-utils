package filesystem

import (
	"io"
	"io/fs"
)

type (
	File interface {
		fs.File
		// TODO: we should probably not define these
		// or at least separate them into File, WritableFile{File; ...}
		SeekerFile
		WriterFile
		//
	}

	SeekerFile interface {
		fs.File
		io.Seeker
	}

	WriterFile interface {
		fs.File
		io.Writer
	}
)
