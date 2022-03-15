package settings

import (
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

// Settings implements the `parameters.Settings` interface
// to generate parameter getters and setters.
type Settings struct{}

func (*Settings) Parameters() parameters.Parameters {
	return nil
}
