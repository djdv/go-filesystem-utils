package cgofuse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/winfsp/cgofuse/fuse"
)

type (
	directoryStream struct {
		fs.ReadDirFile
		entries <-chan filesystem.DirStreamEntry
		context.Context
		context.CancelFunc
		uid, gid uint32
		position int64
	}
	dirEntryWrapper     struct{ fs.DirEntry }
	errorDirectoryEntry struct{ error }
)

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

func (gw *goWrapper) Opendir(path string) (int, uint64) {
	defer gw.systemLock.Access(path)()

	openDirFS, ok := gw.FS.(filesystem.OpenDirFS)
	if !ok {
		if idFS, ok := gw.FS.(filesystem.IDer); ok {
			gw.log.Print("Opendir not supported by provided ", idFS.ID()) // TODO: better message
		} else {
			gw.log.Print("Opendir not supported by provided fs.FS") // TODO: better message
		}
		return -fuse.ENOSYS, errorHandle
	}

	goPath, err := fuseToGo(path)
	if err != nil {
		// TODO: review; POSIX spec - make sure errno is appropriate for this op
		gw.log.Print(err)
		return interpretError(err), errorHandle
	}

	directory, err := openDirFS.OpenDir(goPath)
	if err != nil {
		gw.log.Print(err)
		return interpretError(err), errorHandle
	}

	/* TODO: [Ame]
	Convert the markdown section from [filesystem.md] into a smaller remark. And/or link to it.
	The remark about "needing" IDs is only true for systems where we synthesize them.
	(I.e. for systems that don't natively have/store them; e.g. IPFS' UFS)
	That needs to be stated/corrected.

	## Misc Notes

	### FUSE

	It should be noted somewhere, the behaviour of (Go)`fuse.Getcontext`/(C)`fuse_get_context`.
	None of the implementations have useful documentation for this call, other than saying the pointer to the structure should not be held past the operation call that invoked it.
	The various implementations have varying results. For example, consider the non-exhaustive table below.

	| FreeBSD (fusefs)<br>NetBSD (PUFFS)<br>macOS (FUSE for macOS) | Linux (fuse)       | Windows (WinFSP)   |
	| ------------------------------------------------------------ | ------------------ | ------------------ |
	| opendir: populated                                           | opendir: populated | opendir: populated |
	| readdir: populated                                           | readdir: populated | readdir: NULL      |
	| releasedir: populated                                        | releasedir: NULL   | releasedir: NULL   |

	Inherently, but not via any spec, the context is only required to be populated within operations that create system files and/or check system access. (Without them, you wouldn't be able to implement file systems that adhere to POSIX specifications.)
	i.e. `opendir` must know the UID/GID of the caller in order to check access permissions, but `readdir` does not, since `readdir` implies that the check was already done in `opendir` (as it must receive a valid reference that was previously returned from `opendir`).

	As such, for our `readdir` implementations, we obtain the context during `opendir`, and bind it with the associated handle construct, if it's needed.
	During normal operation it's not, but for systems that support FUSE's "readdirplus" capability, we need the context of the caller who opened the directory at the time of `readdir` operation.
	*/
	var (
		uid, gid, _ = fuse.Getcontext()
		ctx, cancel = context.WithCancel(context.Background())
		dirStream   = &directoryStream{
			ReadDirFile: directory,
			uid:         uid,
			gid:         gid,
			Context:     ctx,
			CancelFunc:  cancel,
		}
	)
	extended, ok := directory.(filesystem.StreamDirFile)
	if ok {
		dirStream.entries = extended.StreamDir(ctx)
	} else {
		dirStream.entries = streamStandardDir(ctx, directory)
	}

	handle, err := gw.fileTable.add(dirStream)
	if err != nil {
		gw.log.Print(err)
		return -fuse.EMFILE, errorHandle
	}

	return operationSuccess, handle
}

func (gw *goWrapper) Releasedir(path string, fh uint64) int {
	errNo, err := gw.fileTable.release(fh)
	if err != nil {
		gw.log.Print(err)
	}
	return errNo
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
		gw.log.Printf(`Directory from file table is not a type from our system: {%T} "%s"`, path)
		return -fuse.EBADF
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
	streamOffset := stream.position
	if ofst == 0 && streamOffset != 0 {
		// TODO: re-open the stream
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
					entStat.Uid = stream.uid
				}
				if entStat.Gid == posixOmittedID {
					entStat.Uid = stream.uid
				}
			}
			if !fill(entry.Name(), entStat, streamOffset) {
				return operationSuccess // fill asked us to stop filling.
			}
		}
	}
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
