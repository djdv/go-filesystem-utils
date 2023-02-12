package filesystem_test

import (
	"context"
	"io/fs"
	"os"
	"strconv"
	"testing"
	"testing/fstest"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

type (
	openFileFSMock struct{ fs.FS }
	streamDirMock  struct {
		fs.ReadDirFile
		context.Context
		context.CancelFunc
		entries []filesystem.StreamDirEntry
	}
)

var (
	_ filesystem.OpenFileFS    = (*openFileFSMock)(nil)
	_ filesystem.StreamDirFile = (*streamDirMock)(nil)
)

func (of *openFileFSMock) OpenFile(name string, _ int, _ fs.FileMode) (fs.File, error) {
	// NOTE: Mock discards arguments.
	// We're only interested in seeing the test coverage trace.
	// The wrapper should follow the [filesystem.OpenFileFS]
	// type-assertion branch, when passed our mock.
	return of.FS.Open(name)
}

func (sd *streamDirMock) StreamDir() <-chan filesystem.StreamDirEntry {
	var (
		ctx     = sd.Context
		entries = make(chan filesystem.StreamDirEntry)
	)
	go func() {
		defer close(entries)
		for _, entry := range sd.entries {
			if ctx.Err() != nil {
				return
			}
			select {
			case entries <- entry:
			case <-ctx.Done():
				return
			}
		}
	}()
	return entries
}

func (sd *streamDirMock) Close() error { sd.CancelFunc(); return nil }

func TestFilesystem(t *testing.T) {
	t.Parallel()
	t.Run("OpenFileFS", openFileFS)
	t.Run("StreamDir", streamDir)
}

func openFileFS(t *testing.T) {
	t.Parallel()
	const fileName = "file"
	testFS := fstest.MapFS{
		fileName: new(fstest.MapFile),
	}

	// Wrapper around standard [fs.File.Open] should succeed
	// with read-only flags.
	stdFSFile, err := filesystem.OpenFile(testFS, fileName, os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	closeFile(t, stdFSFile)

	// Wrapper around standard [fs.File.Open] should /not/ succeed
	// with other flags.
	stdFSFileBad, err := filesystem.OpenFile(testFS, fileName, os.O_RDWR, 0)
	if err == nil {
		t.Error("expected wrapper to deny access with unexpected flags, but got no error")
		if stdFSFileBad != nil {
			closeFile(t, stdFSFileBad)
		}
	}

	// Extension mock should allow additional flags and arguments.
	extendedFS := &openFileFSMock{FS: testFS}
	extendedFSFile, err := filesystem.OpenFile(extendedFS, fileName, os.O_RDWR|os.O_CREATE, 0o777)
	if err != nil {
		t.Fatal(err)
	}
	closeFile(t, extendedFSFile)
}

func streamDir(t *testing.T) {
	t.Parallel()
	const testEntCount = 64
	var (
		testFS   = make(fstest.MapFS, testEntCount)
		testFile = new(fstest.MapFile)
	)
	for i := 0; i < testEntCount; i++ {
		testFS[strconv.Itoa(i)] = testFile
	}
	t.Run("implements", func(t *testing.T) {
		streamDirImplements(t, testFS)
	})
	t.Run("cancels", func(t *testing.T) {
		streamDirCancels(t, testFS)
	})
}

func streamDirImplements(t *testing.T, testFS fstest.MapFS) {
	t.Parallel()
	const count = 16 // Arbitrary buffer size.
	check := func(entries []filesystem.StreamDirEntry) {
		t.Helper()
		if got, want := len(entries), len(testFS); got != want {
			t.Errorf("length mismatch"+
				"\n\tgot: %d"+
				"\n\twant: %d",
				got, want,
			)
		}
	}
	var (
		ctx, cancel = context.WithCancel(context.Background())

		// Values returned utilizing standard [fs.ReadDirFile].
		stdFile        = openRoot(t, testFS)
		stdReadDirFile = assertReadDirFile(t, stdFile)
		stdEntries     = streamEntries(t, ctx, count, stdReadDirFile)

		// Values returned utilizing our extension.
		extendedReadDirFile = &streamDirMock{
			entries: stdEntries,
			Context: ctx, CancelFunc: cancel,
		}
		extensionEntries = streamEntries(t, ctx, count, extendedReadDirFile)
	)
	defer cancel()
	closeFile(t, stdFile)
	closeFile(t, extendedReadDirFile)
	check(stdEntries)
	check(extensionEntries)
}

func streamEntries(t *testing.T, ctx context.Context,
	count int, dir fs.ReadDirFile,
) []filesystem.StreamDirEntry {
	var (
		stream  = filesystem.StreamDir(ctx, count, dir)
		entries = make([]filesystem.StreamDirEntry, 0, cap(stream))
	)
	for entry := range stream {
		if err := entry.Error(); err != nil {
			t.Error(err)
		} else {
			entries = append(entries, entry)
		}
	}
	return entries
}

func streamDirCancels(t *testing.T, testFS fstest.MapFS) {
	t.Parallel()
	const count = 16 // Arbitrary buffer size.
	check := func(entries []filesystem.StreamDirEntry) {
		t.Helper()
		if len(entries) != 0 {
			t.Error("entries returned with canceled context / closed directory")
		}
	}
	{ // Values returned utilizing standard [fs.ReadDirFile].
		var (
			stdFile        = openRoot(t, testFS)
			stdReadDirFile = assertReadDirFile(t, stdFile)
			ctx, cancel    = context.WithCancel(context.Background())
		)
		cancel()
		entries := streamEntries(t, ctx, count, stdReadDirFile)
		closeFile(t, stdReadDirFile)
		check(entries)
	}
	{ // Values returned utilizing our extension.
		var (
			ctx, cancel         = context.WithCancel(context.Background())
			fakeEntries         = []filesystem.StreamDirEntry{nil}
			extendedReadDirFile = &streamDirMock{
				entries: fakeEntries,
				Context: ctx, CancelFunc: cancel,
			}
		)
		defer cancel()
		closeFile(t, extendedReadDirFile)
		extensionEntries := streamEntries(t, ctx, count, extendedReadDirFile)
		check(extensionEntries)
	}
}

func openRoot(t *testing.T, fsys fs.FS) fs.File {
	t.Helper()
	const fsRoot = "."
	root, err := fsys.Open(fsRoot)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func closeFile(t *testing.T, file fs.File) {
	t.Helper()
	if err := file.Close(); err != nil {
		t.Error(err)
	}
}

func assertReadDirFile(t *testing.T, file fs.File) fs.ReadDirFile {
	t.Helper()
	readDirFile, ok := file.(fs.ReadDirFile)
	if !ok {
		t.Fatalf("%T does no implement expected fs.ReadDirFile interface", file)
	}
	return readDirFile
}
