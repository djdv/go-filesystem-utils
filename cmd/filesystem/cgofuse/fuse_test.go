//+build !nofuse

package cgofuse_test

import (
	"context"
	"math"
	"testing"
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
