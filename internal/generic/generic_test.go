package generic_test

import (
	"testing"
)

func TestGeneric(t *testing.T) {
	t.Parallel()
	t.Run("channel", channel)
	t.Run("slice", slice)
}
