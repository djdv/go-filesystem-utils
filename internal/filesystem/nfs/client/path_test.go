package nfs

import (
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
)

func TestPath(t *testing.T) {
	t.Parallel()
	t.Run("link target invalid", _isInvalidLink)
}

func _isInvalidLink(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		path    string
		invalid bool
	}{
		{path: ""},
		{path: filesystem.Root},
		{path: ".."},
		{
			path:    "/POSIX/absolute",
			invalid: true,
		},
		{path: "POSIX/relative"},
		{path: "../POSIX/relative"},
		{
			path:    "C:",
			invalid: true,
		},
		{
			path:    `C:\`,
			invalid: true,
		},
		{
			path:    `C:\DOS`,
			invalid: true,
		},
		{path: `\`},
		{
			path:    `\\server\share`,
			invalid: true,
		},
		{
			path:    `\\server\share\file`,
			invalid: true,
		},
		{
			path:    `//server/share`,
			invalid: true,
		},
		{
			path:    `//server/share/file`,
			invalid: true,
		},
	} {
		var (
			path = test.path
			want = test.invalid
		)
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			got := targetIsInvalid(path)
			if got != want {
				t.Errorf(
					"path is-absolute mismatch"+
						"\n\tgot: %t"+
						"\n\twant:%t\n",
					got, want,
				)
			}
		})
	}
}
