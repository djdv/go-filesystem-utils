package motd

import (
	"fmt"
	"os"
	"sort"
	"sync/atomic"
	"unsafe"

	stringfile "github.com/djdv/go-filesystem-utils/internal/p9p/string"
	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
)

type (
	stringFileMap map[string]*stringfile.File
	directory     struct {
		Names []string
		path  *atomic.Uint64
		p9.QID
		p9.Attr
		templatefs.NoopFile
		stringFileMap
	}
)

// TODO: add a note about this being default / a suggestion; not a hard limit. The file can be initalized elsewhere and we should (must) support this.
const MOTDFilename = "MOTD"

// TODO: consider switching convention: Names -> Components? PathComponents?
// TODO: options:
// - parent atomic-path
// - device id
func NewMOTD(names []string, path *atomic.Uint64) (p9.File, p9.QID) {
	const placeholderDev = p9.Dev(2) // TODO from opts
	motdDir := &directory{
		Names: names,
		path:  path,
		QID: p9.QID{
			Type: p9.TypeDir,
			Path: path.Add(1),
		},
		Attr: p9.Attr{
			Mode: p9.ModeDirectory,
			// UID:  p9.NoUID,
			// GID:  p9.NoGID,
			UID:  0,
			GID:  0,
			RDev: placeholderDev,
		},
		stringFileMap: make(stringFileMap),
	}
	return motdDir, motdDir.QID
}

func (d *directory) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	var (
		qid    = d.QID
		attr   p9.Attr
		filled p9.AttrMask
	)
	if req.Empty() {
		return qid, filled, attr, nil
	}

	if req.Mode {
		attr.Mode, filled.Mode = d.Attr.Mode, true
	}
	if req.UID {
		attr.UID, filled.UID = d.Attr.UID, true
	}
	if req.GID {
		attr.GID, filled.GID = d.Attr.GID, true
	}
	if req.GID {
		attr.GID, filled.GID = d.Attr.GID, true
	}
	if req.RDev {
		attr.RDev, filled.RDev = d.Attr.RDev, true
	}

	return qid, filled, attr, nil
}

func (d *directory) Walk(names []string) ([]p9.QID, p9.File, error) {
	switch nameCount := len(names); nameCount {
	case 0:
		nd := new(directory)
		*nd = *d
		return []p9.QID{nd.QID}, nd, nil
	case 1:
		name := names[0]
		if name == ".." {
			nd := new(directory)
			*nd = *d
			return []p9.QID{nd.QID}, nd, nil
		}
		stringFile, ok := d.stringFileMap[name]
		if !ok {
			// [d9e856ce-c3ca-47be-b2e0-db5213c88c61]
			// TODO: the p9 errors situation needs to be handled upstream
			// currently, it expects this error, and will internally translate it
			// but we want / should be able to return it directly
			// (or whatever makes sense to handle [goerrors.Is] client side)
			// return nil, nil, errors.ENOENT
			return nil, nil, os.ErrNotExist
		}
		return []p9.QID{stringFile.QID}, stringFile, nil
	default:
		return nil, nil, fmt.Errorf("dir: depth max is 1 for now")
	}
}

func (d *directory) Create(name string, mode p9.OpenFlags,
	permissions p9.FileMode, _ p9.UID, _ p9.GID,
) (p9.File, p9.QID, uint32, error) {
	if q, err := d.Mknod(name, permissions|p9.ModeRegular, 0, 0, 0, 0); err != nil {
		return nil, q, 0, err
	}
	_, f, err := d.Walk([]string{name})
	if err != nil {
		return nil, p9.QID{}, 0, err
	}
	qid, n, err := f.Open(mode)
	if err != nil {
		return nil, p9.QID{}, 0, err
	}
	return f, qid, n, nil
}

func (d *directory) Mknod(name string, mode p9.FileMode,
	major uint32, minor uint32,
	uid p9.UID, gid p9.GID,
) (p9.QID, error) {
	if _, ok := d.stringFileMap[name]; ok {
		// see: [d9e856ce-c3ca-47be-b2e0-db5213c88c61]
		// return p9.QID{}, errors.EEXIST
		return p9.QID{}, os.ErrExist
	}
	if !mode.IsRegular() {
		// see: [d9e856ce-c3ca-47be-b2e0-db5213c88c61]
		// return p9.QID{}, errors.EINVAL
		return p9.QID{}, os.ErrInvalid
	}

	stringFile, qid := stringfile.New(append(d.Names, name), d.path)
	d.stringFileMap[name] = stringFile
	return qid, nil
}

func (d *directory) getFilenames() []string {
	var (
		i         int
		fileMap   = d.stringFileMap
		filenames = make([]string, len(fileMap))
	)
	for filename := range fileMap {
		filenames[i] = filename
		i++
	}
	return filenames
}

func (d *directory) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	// TODO: prevent double open
	// [p9.Client] already handles this internally
	// but direct calls to [f] can currently violate the standard.
	return d.QID, uint32(unsafe.Sizeof(p9.Dirent{})), nil
}

func (d *directory) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	var (
		filenames = d.getFilenames()
		fileCount = len(filenames)
	)
	if offset >= uint64(fileCount) {
		return nil, nil
	}
	var (
		dirents p9.Dirents
		end     int
		reqEnd  = int(offset) + int(count)
	)
	if reqEnd < fileCount {
		end = reqEnd
	} else {
		end = fileCount
	}
	dirents = make([]p9.Dirent, end)

	fileMap := d.stringFileMap
	sort.Strings(filenames)

	/*
		fmt.Printf("offset: %v\n", offset)
		fmt.Printf("count: %v\n", count)
		fmt.Printf("fileCount: %v\n", fileCount)
		fmt.Printf("reqEnd: %v\n", reqEnd)
		fmt.Printf("end: %v\n", end)
	*/
	for i, filename := range filenames[offset:end] {
		file := fileMap[filename]
		qid, _, _, err := file.GetAttr(p9.AttrMask{})
		if err != nil {
			return nil, err
		}
		dirents[i] = p9.Dirent{
			QID:    qid,
			Offset: offset + uint64(i+1),
			Type:   qid.Type,
			Name:   filename,
		}
	}
	return dirents, nil
}
