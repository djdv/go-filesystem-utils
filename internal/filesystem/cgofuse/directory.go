//go:build !nofuse

package cgofuse

import (
	"context"
	"errors"
	"io/fs"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/winfsp/cgofuse/fuse"
)

type (
	directoryStream struct {
		fs.ReadDirFile
		// TODO: it might be better to use the fs.DirEntry type for this whole chain
		// and only check the type where these are used.
		// E.g. expect standard directory entries, but check if they're extended
		// with an Error method within readdir.
		// This would eliminate the need for us to wrap the types, higher in the chain.
		// And not depend on extensions as much.
		// ^ The downside of this is that callers might not check for this,
		// and thus miss an error value. So it may not be better.
		// Needs consideration.
		entries <-chan filesystem.DirStreamEntry
		context.Context
		context.CancelFunc
		fuseContext
		position int64
	}
)

func (gw *goWrapper) Mkdir(path string, mode uint32) int {
	defer gw.systemLock.CreateOrDelete(path)()
	if maker, ok := gw.FS.(filesystem.MakeDirectoryFS); ok {
		goPath, err := fuseToGo(path)
		if err != nil {
			return interpretError(err)
		}
		if err := maker.MakeDirectory(goPath); err != nil {
			return interpretError(err)
		}
		return operationSuccess
	}
	return -fuse.ENOSYS
}

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
			entries:     filesystem.StreamDir(ctx, directory),
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
	stream.entries = filesystem.StreamDir(ctx, directory)
	stream.position = 0
	return operationSuccess
}

func (gw *goWrapper) Fsyncdir(path string, datasync bool, fh uint64) int {
	gw.log.Printf("Fsyncdir {%X|%t}%q", fh, datasync, path)
	return -fuse.ENOSYS
}

func (gw *goWrapper) Releasedir(path string, fh uint64) int {
	errNo, err := gw.fileTable.release(fh)
	if err != nil {
		gw.log.Print(err)
	}
	return errNo
}

func (gw *goWrapper) Rmdir(path string) int {
	defer gw.systemLock.CreateOrDelete(path)
	if remover, ok := gw.FS.(filesystem.RemoveDirectoryFS); ok {
		goPath, err := fuseToGo(path)
		if err != nil {
			return interpretError(err)
		}
		if err := remover.RemoveDirectory(goPath); err != nil {
			return interpretError(err)
		}
		return operationSuccess
	}
	return -fuse.ENOSYS
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
		err = fserrors.Join(err, ds.ReadDirFile.Close())
		ds.ReadDirFile = nil
	} else {
		err = fserrors.Join(err, errors.New("directory interface is missing"))
	}
	return err
}
