//go:build nofuse

package unmount

import "github.com/djdv/go-filesystem-utils/internal/filesystem"

func unmarshalFUSE() (filesystem.Host, decodeFunc) {
	return filesystem.Host(""), nil
}
