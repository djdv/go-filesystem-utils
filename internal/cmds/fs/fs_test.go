package fs_test

import (
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/cmds/fs"
)

func TestRoot(t *testing.T) {
	t.Parallel()
	fs.Command() // If it doesn't panic, we're okay.
}
