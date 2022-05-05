package runtime_test

import (
	"testing"
)

func TestRuntime(t *testing.T) {
	t.Parallel()
	t.Run("fields", testFields)
}
