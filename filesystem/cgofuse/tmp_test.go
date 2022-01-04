package cgofuse_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	fuselib "github.com/billziss-gh/cgofuse/fuse"
	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/filesystem/cgofuse"
	ipfs "github.com/djdv/go-filesystem-utils/filesystem/ipfscore"
	chunk "github.com/ipfs/go-ipfs-chunker"
	config "github.com/ipfs/go-ipfs-config"
	files "github.com/ipfs/go-ipfs-files"
	"github.com/ipfs/go-ipfs/core"
	"github.com/ipfs/go-ipfs/core/coreapi"
	"github.com/ipfs/go-ipfs/plugin/loader"
	"github.com/ipfs/go-ipfs/repo/fsrepo"
	gomfs "github.com/ipfs/go-mfs"
	coreiface "github.com/ipfs/interface-go-ipfs-core"
	coreoptions "github.com/ipfs/interface-go-ipfs-core/options"
	corepath "github.com/ipfs/interface-go-ipfs-core/path"
)

type (
	fileHandle = uint64
	errNo      = int
)

// implementation detail: this value is what the fuse library passes to us for anonymous requests (like Getattr)
// we use this same value as the erronious handle value
// (for non-anonymous requests; i.e. returned from a failed Open call, checked in Read and reported as an invalid handle)
// despite being the same value, they are semantically separate depending on the context
const anonymousRequestHandle = fileHandle(math.MaxUint64)

func TestAll(t *testing.T) {
	// TODO: don't pin and don't return the pin
	// just prime the data (add testdir for real but don't pin)
	// return the string to the test dir
	// pass it in to pinsfs; check empty, add pin; check pin
	_, testEnv, node, core, unwind := generateTestEnv(t)
	defer node.Close()
	t.Cleanup(unwind)

	ctx := context.TODO()

	t.Run("IPFS", func(t *testing.T) { testIPFS(ctx, t, testEnv, core, node.FilesRoot) })
	/* TODO
	t.Run("IPNS", func(t *testing.T) { testIPNS(ctx, t, env, iEnv, core) })
	t.Run("FilesAPI", func(t *testing.T) { testMFS(ctx, t, env, iEnv, core) })
	t.Run("PinFS", func(t *testing.T) { testPinFS(ctx, t, env, iEnv, core) })
	t.Run("KeyFS", func(t *testing.T) { testKeyFS(ctx, t, env, iEnv, core) })
	t.Run("FS overlay", func(t *testing.T) { testOverlay(ctx, t, env, iEnv, core) })
	*/
}

func testIPFS(ctx context.Context, t *testing.T, testEnv envData, core coreiface.CoreAPI, filesRoot *gomfs.Root) {
	for _, system := range []struct {
		filesystem.ID
		//filesystem.Interface
		readonly bool
	}{
		{ID: filesystem.IPFS, readonly: true},
		// {ID: filesystem.IPNS},
		// {ID: filesystem.Files},
		// {ID: filesystem.PinFS},
		// {ID: filesystem.KeyFS},
	} {
		//nodeFS, err := manager.NewFileSystem(ctx, system.ID, core, filesRoot)
		nodeFS := ipfs.NewInterface(ctx, core, system.ID)
		hostFS, err := cgofuse.NewFuseInterface(nodeFS)
		if err != nil {
			t.Fatal(err)
		}

		hostFS.Init()

		t.Run("Directory operations", func(t *testing.T) { testDirectories(t, testEnv, hostFS) })
		t.Run("File operations", func(t *testing.T) { testFiles(t, testEnv, core, hostFS) })

		hostFS.Destroy()
	}
}

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

const operationSuccess = 0

type readdirTestDirEnt struct {
	name   string
	offset int64
}

func genFill(slice *[]readdirTestDirEnt) func(name string, stat *fuselib.Stat_t, ofst int64) bool {
	return func(name string, _ *fuselib.Stat_t, ofst int64) bool {
		// buffer is full
		if cap(*slice) == 0 {
			return false
		}
		if len(*slice) == cap(*slice) {
			return false
		}

		// populate
		*slice = append(*slice, readdirTestDirEnt{name, ofst})

		// buffer still has free space?
		return len(*slice) != cap(*slice)
	}
}

func genEndlessFill(slice *[]readdirTestDirEnt) func(name string, stat *fuselib.Stat_t, ofst int64) bool {
	return func(name string, _ *fuselib.Stat_t, ofst int64) bool {
		// always populate
		*slice = append(*slice, readdirTestDirEnt{name, ofst})
		return true
	}
}

func testDirectories(t *testing.T, testEnv envData, fs fuselib.FileSystemInterface) {
	localPath := testEnv[directoryRoot][rootDirectoryTestSetBasic].localPath
	corePath := path.Join("/", testEnv[directoryRoot][rootDirectoryTestSetBasic].corePath.Cid().String())

	// TODO: test Open/Close (prior/independent of readdir)
	// TODO: readdir needs bad behaviour tests (double state transformation, stale offsets, invalid offsets, etc.)
	t.Run("Readdir", func(t *testing.T) {
		testReaddir(t, localPath, corePath, fs)
	})
}

func testOpendir(t *testing.T, path string, fs fuselib.FileSystemInterface) fileHandle {
	errno, fh := fs.Opendir(path)
	if errno != operationSuccess {
		t.Fatalf("failed to open directory %q: %s\n", path, fuselib.Error(errno))
	}
	return fh
}

func testReleasedir(t *testing.T, path string, fh fileHandle, fs fuselib.FileSystemInterface) {
	errno := fs.Releasedir(path, fh)
	if errno != operationSuccess {
		t.Fatalf("failed to release directory %q: %s\n", path, fuselib.Error(errno))
	}
}

func testReaddir(t *testing.T, localPath, corePath string, fs fuselib.FileSystemInterface) {
	// setup
	localDir, err := os.Open(localPath)
	if err != nil {
		t.Fatalf("failed to open local environment: %s\n", err)
	}

	localEntries, err := localDir.Readdirnames(0)
	if err != nil {
		t.Fatalf("failed to read local environment: %s\n", err)
	}
	sort.Strings(localEntries)

	{ // instance 1
		dirHandle := testOpendir(t, corePath, fs)

		// make sure we can read the directory completely, in one call; stopped by `Readdir` itself
		t.Run("all at once (stopped by `Readdir`)", func(t *testing.T) {
			testReaddirAllFS(t, localEntries, fs, corePath, dirHandle)
		})
	}

	{ // instance 2
		dirHandle := testOpendir(t, corePath, fs)

		// make sure we can read the directory completely, in one call; stopped by our `filler` function when we reach the end
		var coreEntries []readdirTestDirEnt
		t.Run("all at once (stopped by us)", func(t *testing.T) {
			coreEntries = testReaddirAllCaller(t, localEntries, fs, corePath, dirHandle)
		})

		// check that reading with an offset replays the stream exactly
		t.Run("with offset", func(t *testing.T) {
			testReaddirOffset(t, coreEntries, fs, corePath, dirHandle)
		})

		// we're done with this instance
		testReleasedir(t, corePath, dirHandle, fs)
	}

	{ // instance 3
		dirHandle := testOpendir(t, corePath, fs)

		// test reading 1 by 1
		t.Run("incremental", func(t *testing.T) {
			testReaddirAllIncremental(t, localEntries, fs, corePath, dirHandle)
		})

		// we only need this for comparison
		coreEntries := testReaddirAllCaller(t, localEntries, fs, corePath, dirHandle)

		// check that reading incrementally with an offset replays the stream exactly
		t.Run("incrementally with offset", func(t *testing.T) {
			testReaddirIncrementalOffset(t, coreEntries, fs, corePath, dirHandle)
		})

		// we're done with this instance
		testReleasedir(t, corePath, dirHandle, fs)
	}
}

func sortEnts(expected []string, have []readdirTestDirEnt) ([]string, []string) {
	// entries are not expected to be sorted from either source
	// we'll make and munge copies so we don't alter the source inputs
	sortedExpectations := make([]string, len(expected))
	copy(sortedExpectations, expected)

	sortedEntries := make([]string, 0, len(expected))
	for _, ent := range have {
		sortedEntries = append(sortedEntries, ent.name)
	}

	// in-place sort actual
	sort.Strings(sortedEntries)
	sort.Strings(sortedExpectations)

	return sortedExpectations, sortedEntries
}

func testReaddirAllFS(t *testing.T, expected []string, fs fuselib.FileSystemInterface, corePath string, fh fileHandle) []readdirTestDirEnt {
	coreEntries := make([]readdirTestDirEnt, 0, len(expected))
	filler := genEndlessFill(&coreEntries)

	const offsetVal = 0
	if errNo := fs.Readdir(corePath, filler, offsetVal, fh); errNo != operationSuccess {
		t.Fatalf("Readdir failed (status: %s) reading {%#x|%q} with offset %d\n", fuselib.Error(errNo), fh, corePath, offsetVal)
	}

	sortedExpectations, sortedCoreEntries := sortEnts(expected, coreEntries)

	// actual comparison
	if !reflect.DeepEqual(sortedExpectations, sortedCoreEntries) {
		t.Fatalf("entries within directory do not match\nexpected:%v\nhave:%v", sortedExpectations, sortedCoreEntries)
	}

	t.Logf("%v\n", coreEntries)
	return coreEntries
}

func testReaddirAllCaller(t *testing.T, expected []string, fs fuselib.FileSystemInterface, corePath string, fh fileHandle) []readdirTestDirEnt {
	coreEntries := make([]readdirTestDirEnt, 0, len(expected))
	filler := genFill(&coreEntries)

	const offsetVal = 0
	if errNo := fs.Readdir(corePath, filler, offsetVal, fh); errNo != operationSuccess {
		t.Fatalf("Readdir failed (status: %s) reading {%#x|%q} with offset %d\n", fuselib.Error(errNo), fh, corePath, offsetVal)
	}

	sortedExpectations, sortedCoreEntries := sortEnts(expected, coreEntries)

	// actual comparison
	if !reflect.DeepEqual(sortedExpectations, sortedCoreEntries) {
		t.Fatalf("entries within directory do not match\nexpected:%v\nhave:%v", sortedExpectations, sortedCoreEntries)
	}

	t.Logf("%v\n", coreEntries)
	return coreEntries
}

func testReaddirOffset(t *testing.T, existing []readdirTestDirEnt, fs fuselib.FileSystemInterface, corePath string, fh fileHandle) {
	partialList := make([]readdirTestDirEnt, 0, len(existing)-1)
	filler := genFill(&partialList)

	offsetVal := existing[0].offset
	// read back the same entries. starting at an offset, contents should match
	if errNo := fs.Readdir(corePath, filler, offsetVal, fh); errNo != operationSuccess {
		t.Fatalf("Readdir failed (status: %s) reading {%#x|%q} with offset %d\n", fuselib.Error(errNo), fh, corePath, offsetVal)
	}

	// providing an offset should replay the stream exactly; no sorting should occur
	if !reflect.DeepEqual(existing[1:], partialList) {
		t.Fatalf("offset entries do not match\nexpected:%v\nhave:%v", existing[1:], partialList)
	}

	t.Logf("%v\n", partialList)
}

func genShortFill(slice *[]readdirTestDirEnt) func(name string, stat *fuselib.Stat_t, ofst int64) bool {
	return func(name string, _ *fuselib.Stat_t, ofst int64) bool {
		*slice = append(*slice, readdirTestDirEnt{name, ofst})
		return false // buffer is full
	}
}

func testReaddirAllIncremental(t *testing.T, expected []string, fs fuselib.FileSystemInterface, corePath string, fh fileHandle) {
	var (
		offsetVal  int64
		entNames   = make([]string, 0, len(expected))
		loggedEnts = make([]readdirTestDirEnt, 0, len(expected))
	)

	for {
		singleEnt := make([]readdirTestDirEnt, 0, 1)
		filler := genShortFill(&singleEnt)

		if errNo := fs.Readdir(corePath, filler, offsetVal, fh); errNo != operationSuccess {
			t.Fatalf("Readdir failed (status: %s) reading {%#x|%q} with offset %d\n", fuselib.Error(errNo), fh, corePath, offsetVal)
		}

		if len(singleEnt) == 0 {
			// Readdir didn't fail but filled in nothing; (equivalent of `readdir() == NULL`)
			break
		}

		if len(singleEnt) != 1 {
			t.Fatalf("Readdir did not respect fill() stop signal (buffer overflowed)")
		}

		t.Logf("rai ent:%s\n", singleEnt[0].name)

		entNames = append(entNames, singleEnt[0].name)
		loggedEnts = append(loggedEnts, singleEnt...)
		offsetVal = singleEnt[0].offset
	}

	// entries are not expected to be sorted from either source
	// we'll make and munge copies so we don't alter the source inputs
	sortedExpectationsAndDreams := make([]string, len(expected))
	copy(sortedExpectationsAndDreams, expected)

	// in-place sort actual
	sort.Strings(entNames)
	sort.Strings(sortedExpectationsAndDreams)

	// actual comparison
	if !reflect.DeepEqual(sortedExpectationsAndDreams, entNames) {
		t.Fatalf("entries within directory do not match\nexpected:%v\nhave:%v", sortedExpectationsAndDreams, entNames)
	}
	t.Logf("%v\n", loggedEnts)
}

func testReaddirIncrementalOffset(t *testing.T, existing []readdirTestDirEnt, fs fuselib.FileSystemInterface, corePath string, fh fileHandle) {
	compareBuffer := make([]readdirTestDirEnt, 0, int64(len(existing)-1))

	for _, ent := range existing {
		offsetVal := ent.offset
		singleEnt := make([]readdirTestDirEnt, 0, 1)
		shortFiller := genShortFill(&singleEnt)

		if errNo := fs.Readdir(corePath, shortFiller, offsetVal, fh); errNo != operationSuccess {
			t.Fatalf("Readdir failed (status: %s) reading {%#x|%q} with offset %d\n", fuselib.Error(errNo), fh, corePath, offsetVal)
		}

		if len(singleEnt) == 0 {
			// Readdir didn't fail but filled in nothing; (equivalent of `readdir() == NULL`)
			break
		}

		if len(singleEnt) != 1 {
			t.Fatalf("Readdir did not respect fill() stop signal (buffer overflowed)")
		}

		compareBuffer = append(compareBuffer, singleEnt[0])
	}

	if !reflect.DeepEqual(existing[1:], compareBuffer) {
		t.Fatalf("offset entries do not match\nexpected:%v\nhave:%v", existing[1:], compareBuffer)
	}

	t.Logf("%v\n", compareBuffer)
}

func testFiles(t *testing.T, testEnv envData, core coreiface.CoreAPI, fs fuselib.FileSystemInterface) {
	// we're specifically interested in semi-static data such as the UID, time, blocksize, permissions, etc.
	statTemplate := testGetattr(t, "/", nil, anonymousRequestHandle, fs)
	statTemplate.Mode &^= fuselib.S_IFMT

	for _, f := range testEnv[directoryTestSetBasic] {
		coreFilePath := path.Join("/", f.corePath.Cid().String())
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

			// HACK: the current implementation doesn't provide these
			expected.Blocks = 0
			expected.Blksize = 0
			// ^

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
