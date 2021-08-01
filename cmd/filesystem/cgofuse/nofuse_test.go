//+build nofuse

package cgofuse_test

import (
	"testing"
)

func TestAll(t *testing.T) {
	t.Run("interface provider stub", testProvider)
}
