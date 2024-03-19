//go:build !nofuse

package unmount

import (
	"encoding/json"

	"github.com/djdv/go-filesystem-utils/internal/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/filesystem/cgofuse"
)

func unmarshalFUSE() (filesystem.Host, decodeFunc) {
	return cgofuse.Host, func(data []byte) (string, error) {
		var mounter cgofuse.Mounter
		if err := json.Unmarshal(data, &mounter); err != nil {
			return "", err
		}
		return mounter.Point, nil
	}
}
