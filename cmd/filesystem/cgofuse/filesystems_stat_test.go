//+build !nofuse

package cgofuse_test

import (
	"reflect"
	"testing"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
)

func testGetattr(t *testing.T, path string, expected *fuselib.Stat_t, fh fileHandle, fs fuselib.FileSystemInterface) *fuselib.Stat_t {
	stat := new(fuselib.Stat_t)
	if errno := fs.Getattr(path, stat, fh); errno != operationSuccess {
		t.Fatalf("failed to get stat for %q: %s\n", path, fuselib.Error(errno))
	}

	if expected == nil {
		t.Log("getattr expected value was empty, not comparing")
	} else if !reflect.DeepEqual(expected, stat) {
		t.Errorf("stats for %q do not match\nexpected:%#v\nhave %#v\n", path, expected, stat)
	}

	return stat
}
