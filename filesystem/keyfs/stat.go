package keyfs

import (
	"io/fs"
	"time"
)

type rootStat time.Time

func (rs *rootStat) Name() string       { return rootName }
func (rs *rootStat) Size() int64        { return 0 } // TODO: This could be the pincount.
func (rs *rootStat) Mode() fs.FileMode  { return fs.ModeDir }
func (rs *rootStat) ModTime() time.Time { return *(*time.Time)(rs) }
func (rs *rootStat) IsDir() bool        { return rs.Mode().IsDir() } // [spec] Don't hardcode this.
func (rs *rootStat) Sys() interface{}   { return rs }

func (ki *keyInterface) Stat(name string) (fs.FileInfo, error) {
	if name == rootName {
		return (*rootStat)(&ki.creationTime), nil
	}

	translated, err := ki.translateName(name)
	if err != nil {
		return nil, err
	}

	return fs.Stat(ki.ipns, translated)
}
