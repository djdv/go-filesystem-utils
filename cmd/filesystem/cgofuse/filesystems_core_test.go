//+build !nofuse

package cgofuse_test

/* TODO: migrate
import (
	"context"
	"testing"

	"github.com/ipfs/go-ipfs/core/commands/filesystem/manager"
	"github.com/ipfs/go-ipfs/core/commands/filesystem/cgofuse"
	"github.com/ipfs/go-ipfs/filesystem"
	gomfs "github.com/ipfs/go-mfs"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
)

func testIPFS(ctx context.Context, t *testing.T, testEnv envData, core coreiface.CoreAPI, filesRoot *gomfs.Root) {
	for _, system := range []struct {
		filesystem.ID
		filesystem.Interface
		readonly bool
	}{
		{ID: filesystem.IPFS},
		// {ID: filesystem.IPNS},
		// {ID: filesystem.Files},
		// {ID: filesystem.PinFS},
		// {ID: filesystem.KeyFS},
	} {
		nodeFS, err := manager.NewFileSystem(ctx, system.ID, core, filesRoot)
		if err != nil {
			t.Fatal(err)
		}

		hostFS, err := fuse.NewFuseInterface(nodeFS)

		hostFS.Init()

		t.Run("Directory operations", func(t *testing.T) { testDirectories(t, testEnv, hostFS) })
		t.Run("File operations", func(t *testing.T) { testFiles(t, testEnv, core, hostFS) })

		hostFS.Destroy()
	}
}
*/
