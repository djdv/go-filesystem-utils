package p9

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"

	fserrors "github.com/djdv/go-filesystem-utils/internal/filesystem/errors"
	"github.com/hugelgupf/p9/p9"
	"github.com/hugelgupf/p9/perrors"
)

type (
	AttacherFile interface {
		p9.Attacher
		p9.File
	}
	chanEmitter[T any] struct {
		context.Context
		ch chan T
		sync.Mutex
	}

	// dataField must be of length 1 with just a key name,
	// or of length 2 with a key and value.
	dataField  []string
	dataTokens []dataField
	fieldType  uint

	openFlags p9.OpenFlags
)

const (
	fileOpened = openFlags(p9.OpenFlagsModeMask + 1)

	keyWord     fieldType = 1
	keyAndValue fieldType = 2
)

func (df dataField) typ() fieldType { return fieldType(len(df)) }

func (of openFlags) withOpenedFlag(mode p9.OpenFlags) openFlags {
	return openFlags(mode.Mode()) | fileOpened
}

func (of openFlags) opened() bool {
	return of&fileOpened != 0
}

func (of openFlags) Mode() p9.OpenFlags {
	return p9.OpenFlags(of).Mode()
}

func (of openFlags) canRead() bool {
	return of.opened() &&
		(of.Mode() == p9.ReadOnly || of.Mode() == p9.ReadWrite)
}

func (of openFlags) canWrite() bool {
	return of.opened() &&
		(of.Mode() == p9.WriteOnly || of.Mode() == p9.ReadWrite)
}

func makeChannelEmitter[T any](ctx context.Context, buffer int) *chanEmitter[T] {
	var (
		ch      = make(chan T, buffer)
		emitter = &chanEmitter[T]{
			Context: ctx,
			ch:      ch,
		}
	)
	emitter.closeWhenDone()
	return emitter
}

func (ce *chanEmitter[T]) closeWhenDone() {
	var (
		ctx = ce.Context
		mu  = &ce.Mutex
		ch  = ce.ch
	)
	go func() {
		<-ctx.Done()
		mu.Lock()
		defer mu.Unlock()
		close(ch)
		ce.ch = nil // See: [emit].
	}()
}

func (ce *chanEmitter[T]) emit(value T) error {
	ce.Mutex.Lock()
	defer ce.Mutex.Unlock()
	var (
		ctx = ce.Context
		ch  = ce.ch
	)
	if ch == nil { // See: [closeWhenDone].
		return ctx.Err()
	}
	select {
	case ch <- value:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func createViaMknod(fsys p9.File, name string, flags p9.OpenFlags,
	permissions p9.FileMode, uid p9.UID, gid p9.GID,
) (_ p9.File, _ p9.QID, _ ioUnit, err error) {
	if qid, err := fsys.Mknod(name, permissions, 0, 0, uid, gid); err != nil {
		return nil, qid, 0, err
	}
	defer func() {
		if err != nil {
			const ulFlags = 0
			err = fserrors.Join(err, fsys.UnlinkAt(name, ulFlags))
		}
	}()
	_, file, err := fsys.Walk([]string{name})
	if err != nil {
		return nil, p9.QID{}, 0, err
	}
	qid, ioUnit, err := file.Open(flags)
	if err != nil {
		return nil, p9.QID{}, 0, err
	}
	return file, qid, ioUnit, nil
}

func MkdirAll(root p9.File, names []string,
	permissions p9.FileMode, uid p9.UID, gid p9.GID,
) (p9.File, error) {
	_, current, err := root.Walk(nil)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		var (
			next, err = getOrMkdir(current, name, permissions, uid, gid)
			cErr      = current.Close()
		)
		if err != nil {
			return nil, fserrors.Join(err, cErr)
		}
		if cErr != nil {
			return nil, fserrors.Join(cErr, next.Close())
		}
		current = next
	}
	return current, nil
}

func getOrMkdir(fsys p9.File, name string, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.File, error) {
	wnames := []string{name}
	_, dir, err := fsys.Walk(wnames)
	if err == nil {
		return dir, nil
	}
	if !errors.Is(err, perrors.ENOENT) {
		return nil, err
	}
	if _, err = fsys.Mkdir(name, permissions, uid, gid); err != nil {
		return nil, err
	}
	_, dir, err = fsys.Walk(wnames)
	return dir, err
}

// mkPreamble makes sure name does not exist
// and may substitute ID values.
// Intended to be called at the beginning of
// mk* functions {mkdir, mknod, etc.}.
func mkPreamble(parent p9.File, name string,
	uid p9.UID, gid p9.GID,
) (p9.UID, p9.GID, error) {
	exists, err := childExists(parent, name)
	if err != nil {
		return p9.NoUID, p9.NoGID, err
	}
	if exists {
		return p9.NoUID, p9.NoGID, perrors.EEXIST
	}
	return maybeInheritIDs(parent, uid, gid)
}

func ReadDir(dir p9.File) (_ p9.Dirents, err error) {
	_, dirClone, err := dir.Walk(nil)
	closeClone := func() {
		if cErr := dirClone.Close(); cErr != nil {
			if err == nil {
				err = cErr
			} else {
				err = fserrors.Join(err, cErr)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	defer closeClone()
	if _, _, err = dirClone.Open(p9.ReadOnly); err != nil {
		return nil, err
	}
	var (
		offset uint64
		ents   p9.Dirents
	)
	for {
		entBuf, err := dirClone.Readdir(offset, math.MaxUint32)
		if err != nil {
			return nil, err
		}
		bufferedEnts := len(entBuf)
		if bufferedEnts == 0 {
			break
		}
		offset = entBuf[bufferedEnts-1].Offset
		ents = append(ents, entBuf...)
	}
	return ents, nil
}

func walkEnt(parent p9.File, ent p9.Dirent) (p9.File, error) {
	wnames := []string{ent.Name}
	_, child, err := parent.Walk(wnames)
	return child, err
}

// ReadAll performs the following sequence on file:
// clone, stat(size), open(read-only), read, close.
func ReadAll(file p9.File) (_ []byte, err error) {
	// TODO: walkgetattr with fallback.
	_, fileClone, err := file.Walk(nil)
	if err != nil {
		return nil, err
	}

	want := p9.AttrMask{Size: true}
	_, valid, attr, err := fileClone.GetAttr(want)
	if err != nil {
		return nil, err
	}
	if !valid.Contains(want) {
		return nil, attrErr(valid, want)
	}

	if _, _, err := fileClone.Open(p9.ReadOnly); err != nil {
		return nil, err
	}
	sr := io.NewSectionReader(fileClone, 0, int64(attr.Size))
	data, err := io.ReadAll(sr)
	return data, fserrors.Join(err, fileClone.Close())
}

func renameAt(oldDir, newDir p9.File, oldName, newName string) error {
	_, file, err := oldDir.Walk([]string{oldName})
	if err != nil {
		return err
	}
	err = rename(file, oldDir, newDir, oldName, newName)
	if cErr := file.Close(); cErr != nil {
		const closeFmt = "could not close old file: %w"
		err = fserrors.Join(err, fmt.Errorf(closeFmt, cErr))
	}
	return err
}

func rename(file, oldDir, newDir p9.File, oldName, newName string) error {
	if err := newDir.Link(file, newName); err != nil {
		return err
	}
	const flags uint32 = 0
	err := oldDir.UnlinkAt(oldName, flags)
	if err != nil {
		if uErr := newDir.UnlinkAt(newName, flags); uErr != nil {
			const unlinkFmt = "could not unlink new file: %w"
			err = fserrors.Join(err, fmt.Errorf(unlinkFmt, uErr))
		}
	}
	return err
}

// TODO better name?
// Flatten returns all files within a directory (recursively).
// It is the callers responsibility to
// close the returned files when done.
func Flatten(dir p9.File) (_ []p9.File, err error) {
	ents, err := ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var (
		files           = make([]p9.File, 0, len(ents))
		closeAllOnError = func() {
			if err == nil {
				return
			}
			for _, file := range files {
				if cErr := file.Close(); cErr != nil {
					err = fserrors.Join(err, cErr)
				}
			}
		}
	)
	defer closeAllOnError()
	for _, ent := range ents {
		file, err := walkEnt(dir, ent)
		if err != nil {
			return nil, err
		}
		if ent.Type == p9.TypeDir {
			subFiles, err := Flatten(file)
			if err != nil {
				return nil, err
			}
			if err := file.Close(); err != nil {
				return nil, err
			}
			files = append(files, subFiles...)
			continue
		}
		files = append(files, file)
	}
	return files, nil
}

func gatherEnts(fsys p9.File, errs chan<- error) (<-chan p9.File, error) {
	ents, err := ReadDir(fsys)
	if err != nil {
		return nil, err
	}
	files := make(chan p9.File, len(ents))
	go func() {
		defer close(files)
		for _, ent := range ents {
			file, err := walkEnt(fsys, ent)
			if err != nil {
				errs <- err
				continue
			}
			files <- file
		}
	}()
	return files, nil
}

func unlinkAllChildren(dir p9.File, errs chan<- error) {
	entries, err := ReadDir(dir)
	if err != nil {
		errs <- err
		return
	}
	const flags = 0
	for _, entry := range entries {
		name := entry.Name
		if err := dir.UnlinkAt(name, flags); err != nil {
			errs <- err
		}
	}
}

// tokenize is for parsing data from Read/Write.
// Returning a list of tokens
// which contain a list of fields.
func tokenize(p []byte) dataTokens {
	var (
		str   = string(p)
		split = strings.FieldsFunc(str, func(r rune) bool {
			return r == '\t' || r == '\r' || r == '\n'
		})
		tokens = make(dataTokens, len(split))
	)
	for i, token := range split {
		fields := strings.Fields(token)
		if len(fields) > 2 { // Preserve spaces in values.
			fields = dataField{
				fields[0],
				strings.Join(fields[1:], " "),
			}
		}
		tokens[i] = fields
	}
	return tokens
}
