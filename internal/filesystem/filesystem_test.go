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

func (sd *streamDirMock) StreamDir(ctx context.Context) <-chan filesystem.StreamDirEntry {
	entries := make(chan filesystem.StreamDirEntry)
	go func() {
		defer close(entries)
		for _, entry := range sd.entries {
			select {
			case entries <- entry:
			case <-ctx.Done():
				return
			}
		}
	}()
	return entries
}

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
	if err := stdFSFile.Close(); err != nil {
		t.Fatal(err)
	}

	// Wrapper around standard [fs.File.Open] should /not/ succeed
	// with other flags.
	stdFSFileBad, err := filesystem.OpenFile(testFS, fileName, os.O_RDWR, 0)
	if err == nil {
		t.Error("expected wrapper to deny access with unexpected flags, but got no error")
		if stdFSFileBad != nil {
			if err := stdFSFileBad.Close(); err != nil {
				t.Errorf("additionally, close failed for returned file: %s", err)
			}
		}
	}

	// Extension mock should allow additional flags and arguments.
	extendedFS := &openFileFSMock{FS: testFS}
	extendedFSFile, err := filesystem.OpenFile(extendedFS, fileName, os.O_RDWR|os.O_CREATE, 0o777)
	if err != nil {
		t.Fatal(err)
	}
	if err := extendedFSFile.Close(); err != nil {
		t.Fatal(err)
	}
}

func streamDir(t *testing.T) {
	t.Parallel()
	const (
		fsRoot       = "."
		testEntCount = 64
	)
	var (
		testFS   = make(fstest.MapFS, testEntCount)
		testFile = new(fstest.MapFile)
		check    = func(entries []filesystem.StreamDirEntry) {
			if got, want := len(entries), len(testFS); got != want {
				t.Errorf("length mismatch"+
					"\n\tgot: %d"+
					"\n\twant: %d",
					got, want,
				)
			}
		}
	)
	for i := 0; i < testEntCount; i++ {
		testFS[strconv.Itoa(i)] = testFile
	}

	// Values returned utilizing standard [fs.ReadDirFile].
	stdFile, err := testFS.Open(fsRoot)
	if err != nil {
		t.Fatal(err)
	}
	stdReadDirFile, ok := stdFile.(fs.ReadDirFile)
	if !ok {
		t.Fatalf("%T does no impliment expected fs.ReadDirFile interface", stdFile)
	}
	stdEntries := streamEntries(t, stdReadDirFile)
	if err := stdFile.Close(); err != nil {
		t.Error(err)
	}
	check(stdEntries)

	// Values returned utilizing our extension.
	var (
		extendedReadDirFile = &streamDirMock{entries: stdEntries}
		extensionEntries    = streamEntries(t, extendedReadDirFile)
	)
	check(extensionEntries)
}

func streamEntries(t *testing.T, dir fs.ReadDirFile) []filesystem.StreamDirEntry {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entries := make([]filesystem.StreamDirEntry, 0)
	for entry := range filesystem.StreamDir(ctx, dir) {
		if err := entry.Error(); err != nil {
			t.Error(err)
		} else {
			entries = append(entries, entry)
		}
	}
	return entries
}
