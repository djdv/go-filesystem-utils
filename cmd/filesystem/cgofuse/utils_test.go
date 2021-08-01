package cgofuse_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	chunk "github.com/ipfs/go-ipfs-chunker"
	config "github.com/ipfs/go-ipfs-config"
	files "github.com/ipfs/go-ipfs-files"
	"github.com/ipfs/go-ipfs/core"
	"github.com/ipfs/go-ipfs/core/coreapi"
	"github.com/ipfs/go-ipfs/plugin/loader"
	"github.com/ipfs/go-ipfs/repo/fsrepo"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	coreoptions "github.com/ipfs/interface-go-ipfs-core/options"
	corepath "github.com/ipfs/interface-go-ipfs-core/path"
)

// TODO: go 1.15; ioutil.TempDir -> t.TempDir

const incantation = "May the bits passing through this device somehow help bring peace to this world"

const (
	rootFileEmpty = iota
	rootFileSmall
	rootFile4MiB
	rootFile8MiB
	rootFile16MiB
	rootFile32MiB
	rootDirectoryEmpty
	rootDirectoryTestSetBasic
)

const (
	directoryRoot = iota
	directoryEmpty
	directoryTestSetBasic
)

type envDataGroup struct {
	localPath string
	corePath  corepath.Resolved
	info      os.FileInfo
}

// (partially) indexed by consts above
// env[directoryRoot][rootFileSmall].localPath
type envData [][]envDataGroup

func testInitNode(ctx context.Context, t *testing.T) (*core.IpfsNode, error) {
	repoPath, err := config.PathRoot()
	if err != nil {
		t.Logf("Failed to find suitable IPFS repo path: %s\n", err)
		t.FailNow()
		return nil, err
	}

	if err := setupPlugins(repoPath); err != nil {
		t.Logf("Failed to initialize IPFS node plugins: %s\n", err)
		t.FailNow()
		return nil, err
	}

	conf, err := config.Init(ioutil.Discard, 2048)
	if err != nil {
		t.Logf("Failed to construct IPFS node config: %s\n", err)
		t.FailNow()
		return nil, err
	}

	if err := fsrepo.Init(repoPath, conf); err != nil {
		t.Logf("Failed to construct IPFS node repo: %s\n", err)
		t.FailNow()
		return nil, err
	}

	repo, err := fsrepo.Open(repoPath)
	if err != nil {
		t.Logf("Failed to open newly initialized IPFS repo: %s\n", err)
		t.FailNow()
		return nil, err
	}

	return core.NewNode(ctx, &core.BuildCfg{
		Online:                      false,
		Permanent:                   false,
		DisableEncryptedConnections: true,
		Repo:                        repo,
	})
}

func setupPlugins(path string) error {
	// Load plugins. This will skip the repo if not available.
	plugins, err := loader.NewPluginLoader(filepath.Join(path, "plugins"))
	if err != nil {
		return fmt.Errorf("error loading plugins: %s", err)
	}

	if err := plugins.Initialize(); err != nil {
		return fmt.Errorf("error initializing plugins: %s", err)
	}

	if err := plugins.Inject(); err != nil {
		return fmt.Errorf("error injecting plugins: %s", err)
	}

	return nil
}

// XXX: everything in here is order dependant; matches against the rootX consts
func generateEnvData(t *testing.T, ctx context.Context, core coreiface.CoreAPI) (string, string, envData) {
	testDir, err := ioutil.TempDir("", "ipfs-")
	if err != nil {
		t.Fatalf("failed to create temporary directory: %s", err)
	}

	// make a bunch of junk and stuff it in a root array
	junkFiles := generateTestGarbage(t, testDir, core)
	envRoot := make([]envDataGroup, len(junkFiles)+4)
	copy(envRoot[rootFileSmall+1:], junkFiles)

	{
		path := filepath.Join(testDir, "empty")
		fi, corePath := wrapTestFile(t, path, []byte(nil), core)

		envRoot[rootFileEmpty] = envDataGroup{
			localPath: path,
			info:      fi,
			corePath:  corePath,
		}
	}

	{
		path := filepath.Join(testDir, "small")
		fi, corePath := wrapTestFile(t, path, []byte(incantation), core)
		envRoot[rootFileSmall] = envDataGroup{
			localPath: path,
			info:      fi,
			corePath:  corePath,
		}
	}

	// assign the root to the env array
	env := make([][]envDataGroup, 3)
	env[directoryRoot] = envRoot

	// make some more subdirectories and assign them too
	{
		testSubEmpty, err := ioutil.TempDir(testDir, "ipfs-")
		if err != nil {
			t.Fatalf("failed to create temporary directory: %s", err)
		}
		fi, corePath := wrapTestDir(t, testSubEmpty, core)

		single := envDataGroup{
			localPath: testSubEmpty,
			info:      fi,
			corePath:  corePath,
		}

		envRoot[rootDirectoryEmpty] = single
		env[directoryEmpty] = []envDataGroup{single}
	}

	{
		testSubDir, err := ioutil.TempDir(testDir, "ipfs-")
		if err != nil {
			t.Fatalf("failed to create temporary directory: %s", err)
		}
		subJunkFiles := generateTestGarbage(t, testSubDir, core)

		fi, corePath := wrapTestDir(t, testSubDir, core)
		envRoot[rootDirectoryTestSetBasic] = envDataGroup{
			localPath: testSubDir,
			info:      fi,
			corePath:  corePath,
		}

		env[directoryTestSetBasic] = append([]envDataGroup{
			envRoot[rootFileEmpty],
			envRoot[rootFileSmall],
		}, subJunkFiles...,
		)
	}

	iPath, err := pinAddDir(ctx, core, testDir)
	if err != nil {
		t.Fatalf("failed to pin test data %q: %s", testDir, err)
	}

	return testDir, iPath.Cid().String(), env
}

func pinAddDir(ctx context.Context, core coreiface.CoreAPI, path string) (corepath.Resolved, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	node, err := files.NewSerialFile(path, false, fi)
	if err != nil {
		return nil, err
	}

	iPath, err := core.Unixfs().Add(ctx, node.(files.Directory), coreoptions.Unixfs.Pin(true))
	if err != nil {
		return nil, err
	}
	return iPath, nil
}

func generateTestGarbage(t *testing.T, tempDir string, core coreiface.CoreAPI) []envDataGroup {
	randDev := rand.New(rand.NewSource(time.Now().UnixNano()))

	junk := [...]int{4, 8, 16, 32}
	junkFiles := make([]envDataGroup, 0, len(junk))

	// generate files of different sizes filled with random data
	for _, size := range junk {
		buf := make([]byte, size<<(10*2))
		if _, err := randDev.Read(buf); err != nil {
			t.Fatalf("failed to read from random reader: %s\n", err)
		}

		path := filepath.Join(tempDir, fmt.Sprintf("%dMiB", size))
		fi, corePath := wrapTestFile(t, path, buf, core)

		junkFiles = append(junkFiles, envDataGroup{
			info:      fi,
			localPath: path,
			corePath:  corePath,
		})
	}

	{
		// generate a file that fits perfectly in a single block
		path := filepath.Join(tempDir, "aligned")
		buf := make([]byte, chunk.DefaultBlockSize, chunk.DefaultBlockSize+1)
		fi, corePath := wrapTestFile(t, path, buf, core)
		junkFiles = append(junkFiles, envDataGroup{
			info:      fi,
			localPath: path,
			corePath:  corePath,
		})

		// generate a file that doesn't fit cleanly in a single block
		path = filepath.Join(tempDir, "misaligned")
		buf = append(buf, 0)
		fi, corePath = wrapTestFile(t, path, buf, core)
		junkFiles = append(junkFiles, envDataGroup{
			info:      fi,
			localPath: path,
			corePath:  corePath,
		})
	}

	return junkFiles
}

func dumpAndStat(path string, buf []byte) (os.FileInfo, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}

	_, err = f.Write(buf)
	if err != nil {
		return nil, err
	}

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	if err := f.Close(); err != nil {
		return nil, err
	}

	return fi, nil
}

func wrapTestFile(t *testing.T, path string, buf []byte, core coreiface.CoreAPI) (os.FileInfo, corepath.Resolved) {
	fi, err := dumpAndStat(path, buf)
	if err != nil {
		t.Fatalf("failed to create local file %q: %s\n", path, err)
	}

	filesNode, err := files.NewSerialFile(path, false, fi)
	if err != nil {
		t.Fatalf("failed to wrap local file %q: %s\n", path, err)
	}

	corePath, err := core.Unixfs().Add(context.TODO(), filesNode.(files.File), coreoptions.Unixfs.HashOnly(true))
	if err != nil {
		t.Fatalf("failed to hash local file %q: %s\n", path, err)
	}
	return fi, corePath
}

func wrapTestDir(t *testing.T, path string, core coreiface.CoreAPI) (os.FileInfo, corepath.Resolved) {
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat local directory %q: %s\n", path, err)
	}
	filesNode, err := files.NewSerialFile(path, false, fi)
	if err != nil {
		t.Fatalf("failed to wrap local directory %q: %s\n", path, err)
	}

	corePath, err := core.Unixfs().Add(context.TODO(), filesNode.(files.Directory), coreoptions.Unixfs.HashOnly(true))
	if err != nil {
		t.Fatalf("failed to hash local directory %q: %s\n", path, err)
	}
	return fi, corePath
}

// TODO: see if we can circumvent import cycle hell and not have to reconstruct the node for each filesystem test
func generateTestEnv(t *testing.T) (string, envData, *core.IpfsNode, coreiface.CoreAPI, func()) {
	// environment setup
	origPath := os.Getenv("IPFS_PATH")

	unwindStack := make([]func(), 0)
	unwind := func() {
		for i := len(unwindStack) - 1; i > -1; i-- {
			unwindStack[i]()
		}
	}

	repoDir, err := ioutil.TempDir("", "ipfs-nodeBinding")
	if err != nil {
		t.Errorf("Failed to create repo directory: %s\n", err)
	}

	unwindStack = append(unwindStack, func() {
		if err = os.RemoveAll(repoDir); err != nil {
			t.Errorf("Failed to remove test repo directory: %s\n", err)
		}
	})

	if err = os.Setenv("IPFS_PATH", repoDir); err != nil {
		t.Logf("Failed to set IPFS_PATH: %s\n", err)
		unwind()
		t.FailNow()
	}

	unwindStack = append(unwindStack, func() {
		if err = os.Setenv("IPFS_PATH", origPath); err != nil {
			t.Errorf("Failed to reset IPFS_PATH: %s\n", err)
		}
	})

	testCtx, testCancel := context.WithCancel(context.Background())
	unwindStack = append(unwindStack, testCancel)

	// node actual
	node, err := testInitNode(testCtx, t)
	if err != nil {
		t.Logf("Failed to construct IPFS node: %s\n", err)
		unwind()
		t.FailNow()
	}

	unwindStack = append(unwindStack, func() {
		if err := node.Close(); err != nil {
			t.Errorf("Failed to close node:%s", err)
		}
	})

	coreAPI, err := coreapi.NewCoreAPI(node)
	if err != nil {
		t.Logf("Failed to construct CoreAPI: %s\n", err)
		unwind()
		t.FailNow()
	}

	// add data to some local path and to the node
	testDir, testPin, testEnv := generateEnvData(t, testCtx, coreAPI)
	if err != nil {
		t.Logf("Failed to construct IPFS test environment: %s\n", err)
		unwind()
		t.FailNow()
	}

	unwindStack = append(unwindStack, func() {
		if err := os.RemoveAll(testDir); err != nil {
			t.Errorf("failed to remove local test data dir %q: %s", testDir, err)
		}
	})

	return testPin, testEnv, node, coreAPI, unwind
}
