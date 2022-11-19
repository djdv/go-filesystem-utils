package cgofuse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/winfsp/cgofuse/fuse"
)

type (
	directoryStream struct {
		fs.ReadDirFile
		entries <-chan filesystem.DirStreamEntry
		context.Context
		context.CancelFunc
		fuseContext
		position int64
	}
	dirEntryWrapper     struct{ fs.DirEntry }
	errorDirectoryEntry struct{ error }
)

func (gw *goWrapper) Opendir(path string) (int, uint64) {
	defer gw.systemLock.Access(path)()
	directory, err := openDir(gw.FS, path)
	if err != nil {
		gw.log.Printf(`%s - "%s"`, err, path)
		return interpretError(err), errorHandle
	}
	var (
		// NOTE: `fuse_get_context` is only required to
		// return valid data within certain operations.
		// `openddir` is one such operation.
		// Some implementations may return valid data within `readdir`
		// but this can not be relied on.
		// As such, we store it with the stream here, for re-use there.
		// TODO: We should accept these values as options
		// and only use the fuse context if not provided.
		// I.e. caller of the wrapper constructor can define what UID+GID we should use.
		uid, gid, _ = fuse.Getcontext()
		ctx, cancel = context.WithCancel(context.Background())
		dirStream   = &directoryStream{
			Context:     ctx,
			CancelFunc:  cancel,
			ReadDirFile: directory,
			entries:     getDirStream(ctx, directory),
			fuseContext: fuseContext{
				uid: uid,
				gid: gid,
			},
		}
	)
	handle, err := gw.fileTable.add(dirStream)
	if err != nil {
		gw.log.Print(err)
		// TODO: the file table should return an error value
		// that maps to this POSIX error.
		return -fuse.EMFILE, errorHandle
	}
	return operationSuccess, handle
}

func openDir(fsys fs.FS, path string) (fs.ReadDirFile, error) {
	goPath, err := fuseToGo(path)
	if err != nil {
		return nil, err
	}
	file, err := fsys.Open(goPath)
	if err != nil {
		return nil, err
	}
	directory, ok := file.(fs.ReadDirFile)
	if !ok {
		return nil, fserrors.New(fserrors.NotDir)
	}
	return directory, nil
}

func getDirStream(ctx context.Context, directory fs.ReadDirFile) <-chan filesystem.DirStreamEntry {
	if dirStreamer, ok := directory.(filesystem.StreamDirFile); ok {
		return dirStreamer.StreamDir(ctx)
	}
	return streamStandardDir(ctx, directory)
}

func (gw *goWrapper) Readdir(path string,
	fill func(name string, stat *fuse.Stat_t, ofst int64) bool,
	ofst int64,
	fh uint64,
) int {
	defer gw.systemLock.Access(path)()
	if fh == errorHandle {
		gw.log.Print(fuse.Error(-fuse.EBADF))
		return -fuse.EBADF
	}
	directoryHandle, err := gw.fileTable.get(fh)
	if err != nil {
		gw.log.Print(fuse.Error(-fuse.EBADF))
		return -fuse.EBADF
	}
	stream, ok := directoryHandle.goFile.(*directoryStream)
	if !ok {
		// TODO: [Ame] better wording; include the expected type as well (got, want).
		// ^ Remember that fmt is funny about interface types and will print a literal `nil`.
		// I forget the trick to coerce it into printing the type. Requires reflection?
		gw.log.Printf(`Directory from file table is not a type from our system: {%T} "%s"`,
			directoryHandle.goFile, path,
		)
		// TODO: [leaks] [S&P]
		// Trace what FUSE does here when it encounters `EBADF`.
		// If it doesn't call `close` itself, we will have to
		// remove this descriptor manually from the file table.
		return -fuse.EBADF
	}
	streamOffset := stream.position
	if ofst == 0 && streamOffset != 0 {
		if errorCode := gw.rewinddir(stream, path); errorCode != operationSuccess {
			return errorCode
		}
	}
	var (
		entries = stream.entries
		ctx     = stream.Context
	)
	if ctx == nil {
		gw.log.Printf(`Directory from file table is missing stream context "%s"`, path)
		return -fuse.EIO
	}
	if entries == nil {
		gw.log.Printf(`Directory from file table is missing stream entries "%s"`, path)
		return -fuse.EIO
	}
	for {
		select {
		case <-ctx.Done():
			return -fuse.EBADF
		case entry, ok := <-entries:
			if !ok {
				return operationSuccess
			}
			streamOffset++
			stream.position = streamOffset
			if err := entry.Error(); err != nil {
				gw.log.Printf(`%s : "%s"`, err, path)
				return -fuse.ENOENT
			}
			entStat, err := dirStat(entry)
			if err != nil {
				gw.log.Printf(`%s : "%s"`, err, path)
				return -fuse.EIO
			}
			if entStat != nil {
				if entStat.Uid == posixOmittedID {
					entStat.Uid = stream.fuseContext.uid
				}
				if entStat.Gid == posixOmittedID {
					entStat.Uid = stream.fuseContext.uid
				}
			}
			if !fill(entry.Name(), entStat, streamOffset) {
				return operationSuccess // fill asked us to stop filling.
			}
		}
	}
}

// TODO: clean this up. Error handling need to be less wonky.
// Field assignment could probably be handled differently.
// NOTE: See SUSv4;BSi7 `rewinddir`.
// FUSE should translate those into (FUSE) `readdir` with offset 0
// ^ TODO: (Re)validate that this is true. Include it in CGO tests as well.
// ^^ With funny business. Directory contents should change between calls.
// opendidr; readdir; modify dir contents; rewinddir; readdir; closedir
func (gw *goWrapper) rewinddir(stream *directoryStream, path string) int {
	if cancel := stream.CancelFunc; cancel != nil {
		cancel()
	} else {
		gw.log.Printf(`Directory from file table is missing stream context CancelFunc "%s"`, path)
		return -fuse.EIO
	}
	directory, err := openDir(gw.FS, path)
	if err != nil {
		return interpretError(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stream.ReadDirFile = directory
	stream.Context = ctx
	stream.CancelFunc = cancel
	stream.entries = getDirStream(ctx, directory)
	stream.position = 0
	return operationSuccess
}

func (gw *goWrapper) Releasedir(path string, fh uint64) int {
	errNo, err := gw.fileTable.release(fh)
	if err != nil {
		gw.log.Print(err)
	}
	return errNo
}

func (ds *directoryStream) Close() (err error) {
	// TODO: can we make this less gross but still safe?
	if cancel := ds.CancelFunc; cancel != nil {
		cancel()
		ds.CancelFunc = nil
		ds.entries = nil
	} else {
		err = errors.New("directory canceler is missing")
	}
	if dirFile := ds.ReadDirFile; dirFile != nil {
		err = errors.Join(err, ds.ReadDirFile.Close())
		ds.ReadDirFile = nil
	} else {
		err = errors.Join(err, errors.New("directory interface is missing"))
	}
	return err
}

func (dirEntryWrapper) Error() error { return nil }

func (ed *errorDirectoryEntry) Name() string               { return "" }
func (ed *errorDirectoryEntry) Error() error               { return ed.error }
func (ed *errorDirectoryEntry) Info() (fs.FileInfo, error) { return nil, ed.error }
func (*errorDirectoryEntry) Type() fs.FileMode             { return fs.ModeDir }
func (*errorDirectoryEntry) IsDir() bool                   { return true }

// TODO: different name? something wrapDir? transform something-something?
func streamStandardDir(ctx context.Context,
	directory fs.ReadDirFile,
) <-chan filesystem.DirStreamEntry {
	stream := make(chan filesystem.DirStreamEntry)
	go func() {
		defer close(stream)
		for {
			var (
				entry     filesystem.DirStreamEntry
				ents, err = directory.ReadDir(1)
			)
			switch {
			case err != nil:
				if errors.Is(err, io.EOF) {
					return
				}
				entry = &errorDirectoryEntry{error: err}
			case len(ents) != 1:
				// TODO: real error message
				err := fmt.Errorf("unexpected count for [fs.ReadDir]"+
					"\n\tgot: %d"+
					"\n\twant: %d",
					len(ents), 1,
				)
				entry = &errorDirectoryEntry{error: err}
			default:
				entry = dirEntryWrapper{DirEntry: ents[0]}
			}
			select {
			case <-ctx.Done():
				return
			case stream <- entry:
			}
		}
	}()
	return stream
}
