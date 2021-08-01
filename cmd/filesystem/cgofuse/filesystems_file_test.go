//+build !nofuse

package cgofuse_test

import (
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
	chunk "github.com/ipfs/go-ipfs-chunker"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

func testFiles(t *testing.T, testEnv envData, core coreiface.CoreAPI, fs fuselib.FileSystemInterface) {
	// we're specifically interested in semi-static data such as the UID, time, blocksize, permissions, etc.
	statTemplate := testGetattr(t, "/", nil, anonymousRequestHandle, fs)
	statTemplate.Mode &^= fuselib.S_IFMT

	for _, f := range testEnv[directoryTestSetBasic] {
		coreFilePath := f.corePath.Cid().String()
		t.Logf("file: %q:%q\n", f.localPath, f.corePath)

		t.Run("Open+Release", func(t *testing.T) {
			// TODO: test a bunch of scenarios/flags as separate runs here
			// t.Run("with O_CREAT"), "Write flags", etc...

			expected := new(fuselib.Stat_t)
			*expected = *statTemplate
			expected.Mode |= fuselib.S_IFREG
			expected.Size = f.info.Size()

			// NOTE: UFS doesn't seem to count the first block; i.e. Blocks == 1 will never be returned
			if expected.Size <= chunk.DefaultBlockSize {
				expected.Blksize = 0
				expected.Blocks = 0
			} else {
				expected.Blksize = chunk.DefaultBlockSize
				expected.Blocks = expected.Size / expected.Blksize
				if expected.Size%expected.Blksize != 0 {
					expected.Blocks++ // remaining bits will require an additional block
				}
			}

			testGetattr(t, coreFilePath, expected, anonymousRequestHandle, fs)

			fh := testOpen(t, coreFilePath, fuselib.O_RDONLY, fs)
			testRelease(t, coreFilePath, fh, fs)
		})

		localFilePath := f.localPath
		mirror, err := os.Open(localFilePath)
		if err != nil {
			t.Fatalf("failed to open local file %q: %s\n", localFilePath, err)
		}

		t.Run("Read", func(t *testing.T) {
			fh := testOpen(t, coreFilePath, fuselib.O_RDONLY, fs)
			testRead(t, coreFilePath, mirror, fh, fs)
		})
		if err := mirror.Close(); err != nil {
			t.Fatalf("failed to close local file %q: %s\n", localFilePath, err)
		}
	}
}

func testOpen(t *testing.T, path string, flags int, fs fuselib.FileSystemInterface) fileHandle {
	errno, fh := fs.Open(path, flags)
	if errno != operationSuccess {
		t.Fatalf("failed to open file %q: %s\n", path, fuselib.Error(errno))
	}
	return fh
}

func testRelease(t *testing.T, path string, fh fileHandle, fs fuselib.FileSystemInterface) errNo {
	errno := fs.Release(path, fh)
	if errno != operationSuccess {
		t.Fatalf("failed to release file %q: %s\n", path, fuselib.Error(errno))
	}
	return errno
}

func testRead(t *testing.T, path string, mirror *os.File, fh fileHandle, fs fuselib.FileSystemInterface) {
	t.Run("all", func(t *testing.T) {
		testReadAll(t, path, mirror, fh, fs)
	})

	if _, err := mirror.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
}

func testReadAll(t *testing.T, path string, mirror *os.File, fh fileHandle, fs fuselib.FileSystemInterface) {
	expected, err := ioutil.ReadAll(mirror)
	if err != nil {
		t.Fatalf("failed to read mirror contents: %s\n", err)
	}

	fullBuff := make([]byte, len(expected))

	readRet := fs.Read(path, fullBuff, 0, fh)
	if readRet < 0 {
		t.Fatalf("failed to read %q: %s\n", path, fuselib.Error(readRet))
	}

	// FIXME: [temporary] don't assume full reads in one shot; this isn't spec compliant
	// we need to loop until EOF
	if readRet != len(expected) || readRet != len(fullBuff) {
		t.Fatalf("read bytes does not match actual length of bytes buffer for %q:\nexpected:%d\nhave:%d\n", path, len(expected), readRet)
	}

	big := len(expected) > 1024

	if !reflect.DeepEqual(expected, fullBuff) {
		if big {
			t.Fatalf("contents for %q do not match:\nexpected to read %d bytes but read %d bytes\n", path, len(expected), readRet)
		}
		t.Fatalf("contents for %q do not match:\nexpected:%v\nhave:%v\n", path, expected, fullBuff)
	}

	if big {
		t.Logf("read %d bytes\n", readRet)
	} else {
		t.Logf("%s\n", fullBuff)
	}
}
