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
	"github.com/multiformats/go-multiaddr"
)

type (
	chanEmitter[T any] struct {
		context.Context
		ch chan T
		sync.Mutex
	}
	result[T any] struct {
		error
		value T
	}
	direntResult   = result[p9.Dirent]
	fileResult     = result[p9.File]
	maddrResult    = result[multiaddr.Multiaddr]
	connInfoResult = result[ConnInfo]
	stringResult   = result[string]

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

	// NOTE: [2023.01.02]
	// The reference documentation and implementation
	// do not specify which error number to use.
	// If this value seems incorrect, request to change it.
	fidOpenedErr = perrors.EBUSY
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

func sendSingle[T any](value T) <-chan T {
	buffer := make(chan T, 1)
	buffer <- value
	close(buffer)
	return buffer
}

func sendResult[T any, R result[T]](ctx context.Context, results chan<- R, res R) bool {
	select {
	case results <- res:
		return true
	case <-ctx.Done():
		return false
	}
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

func ReadDir(dir p9.File) (p9.Dirents, error) {
	var (
		ents        p9.Dirents
		ctx, cancel = context.WithCancel(context.Background())
	)
	defer cancel()
	for result := range getDirents(ctx, dir) {
		if err := result.error; err != nil {
			return nil, err
		}
		ents = append(ents, result.value)
	}
	return ents, nil
}

func getDirents(ctx context.Context, dir p9.File) <-chan direntResult {
	return mapDirPipeline(ctx, dir, getDirentsPipeline)
}

func getDirentsPipeline(ctx context.Context, dir p9.File, wg *sync.WaitGroup, results chan<- direntResult) {
	defer wg.Done()
	if _, _, err := dir.Open(p9.ReadOnly); err != nil {
		sendResult(ctx, results, direntResult{error: err})
		return
	}
	var offset uint64
	for {
		entires, err := dir.Readdir(offset, math.MaxUint32)
		if err != nil {
			sendResult(ctx, results, direntResult{error: err})
			return
		}
		entryCount := len(entires)
		if entryCount == 0 {
			return
		}
		for _, entry := range entires {
			if !sendResult(ctx, results, direntResult{value: entry}) {
				return
			}
		}
		offset = entires[entryCount-1].Offset
	}
}

// getDirFiles retrieves all files within the directory (1 layer deep).
// It is the callers responsibility to close the returned files when done.
func getDirFiles(ctx context.Context, dir p9.File) <-chan fileResult {
	return mapDirPipeline(ctx, dir, getDirFilesPipeline)
}

func getDirFilesPipeline(ctx context.Context, dir p9.File, wg *sync.WaitGroup, results chan<- fileResult) {
	defer wg.Done()
	processEntry := func(result direntResult) {
		defer wg.Done()
		if err := result.error; err != nil {
			sendResult(ctx, results, fileResult{error: err})
			return
		}
		var (
			entry     = result.value
			file, err = walkEnt(dir, entry)
		)
		if !sendResult(ctx, results, fileResult{value: file, error: err}) {
			if file != nil {
				file.Close() // Ignore the error (no receivers).
			}
		}
	}
	for result := range getDirents(ctx, dir) {
		wg.Add(1)
		go processEntry(result)
	}
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

// flattenDir returns all files within a directory (recursively).
// It is the callers responsibility to close the returned files when done.
func flattenDir(ctx context.Context, dir p9.File) <-chan fileResult {
	return mapDirPipeline(ctx, dir, flattenPipeline)
}

func flattenPipeline(ctx context.Context, dir p9.File,
	wg *sync.WaitGroup, results chan<- fileResult,
) {
	defer wg.Done()
	processEntry := func(result direntResult) {
		defer wg.Done()
		if err := result.error; err != nil {
			sendResult(ctx, results, fileResult{error: err})
			return
		}
		var (
			entry     = result.value
			file, err = walkEnt(dir, entry)
		)
		if entry.Type == p9.TypeDir {
			const recurAndClose = 2
			wg.Add(recurAndClose)
			go func() {
				defer wg.Done()
				flattenPipeline(ctx, file, wg, results)
				if err := file.Close(); err != nil {
					sendResult(ctx, results, fileResult{error: err})
				}
			}()
			return
		}
		if !sendResult(ctx, results, fileResult{value: file, error: err}) {
			if file != nil {
				file.Close() // Ignore the error (no receivers).
			}
		}
	}
	for entryResult := range getDirents(ctx, dir) {
		wg.Add(1)
		go processEntry(entryResult)
	}
}

func findFiles(ctx context.Context, root p9.File, name string) <-chan fileResult {
	return mapDirPipeline(ctx, root, func(ctx context.Context, dir p9.File,
		wg *sync.WaitGroup, results chan<- fileResult,
	) {
		findFilesPipeline(ctx, dir, name, wg, results)
	})
}

// findFilesPipeline recursively searches the `root`
// for any files named `name`.
func findFilesPipeline(ctx context.Context, root p9.File, name string, wg *sync.WaitGroup, results chan<- fileResult) {
	defer wg.Done()
	processEntry := func(result direntResult) {
		defer wg.Done()
		if err := result.error; err != nil {
			sendResult(ctx, results, fileResult{error: err})
			return
		}
		entry := result.value
		if entry.Name == name {
			file, err := walkEnt(root, entry)
			if !sendResult(ctx, results, fileResult{value: file, error: err}) {
				if file != nil {
					file.Close() // Ignore the error (no receivers).
				}
				return
			}
		}
		if entry.Type != p9.TypeDir {
			return
		}
		dir, err := walkEnt(root, entry)
		if err != nil {
			sendResult(ctx, results, fileResult{error: err})
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			var recurWg sync.WaitGroup
			recurWg.Add(1)
			findFilesPipeline(ctx, dir, name, &recurWg, results)
			recurWg.Wait()
			if err := dir.Close(); err != nil {
				sendResult(ctx, results, fileResult{error: err})
			}
		}()
	}
	for entryResult := range getDirents(ctx, root) {
		wg.Add(1)
		go processEntry(entryResult)
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

func unlinkChildSync(link *linkSync) error {
	link.mu.Lock()
	defer link.mu.Unlock()
	_, clone, err := link.parent.Walk(nil)
	if err != nil {
		return err
	}
	const flags = 0
	return fserrors.Join(
		clone.UnlinkAt(link.child, flags),
		clone.Close(),
	)
}

func aggregateResults[T any, R result[T]](cancel context.CancelFunc, results <-chan R) ([]T, error) {
	// Conversion necessary until
	// golang/go #48522 is resolved.
	type rc = result[T]
	var (
		values = make([]T, 0, cap(results))
		errs   []error
	)
	for result := range results {
		if err := rc(result).error; err != nil {
			cancel()
			errs = append(errs, err)
			continue
		}
		values = append(values, rc(result).value)
	}
	if errs != nil {
		return nil, fserrors.Join(errs...)
	}
	return values, nil
}

func mapDirPipeline[
	T any,
	P func(context.Context, p9.File, *sync.WaitGroup, chan<- result[T]),
](ctx context.Context,
	dir p9.File,
	pipeline P,
) <-chan result[T] {
	// TODO: In Go 1.21 this can go into the type parameters list.
	// Go 1.20.4 does not see it as a matching type (despite
	// the alias working all the same).
	type R = result[T]
	_, clone, err := dir.Walk(nil)
	if err != nil {
		return sendSingle(R{error: err})
	}
	var (
		wg      sync.WaitGroup
		results = make(chan R)
	)
	wg.Add(1)
	go pipeline(ctx, clone, &wg, results)
	go func() {
		wg.Wait()
		if err := clone.Close(); err != nil {
			sendResult(ctx, results, R{error: err})
		}
		close(results)
	}()
	return results
}

func unwind(err error, funcs ...func() error) error {
	var errs []error
	for _, fn := range funcs {
		if fnErr := fn(); fnErr != nil {
			errs = append(errs, fnErr)
		}
	}
	if errs == nil {
		return err
	}
	return fserrors.Join(append([]error{err}, errs...)...)
}
