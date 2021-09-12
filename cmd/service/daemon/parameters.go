package daemon

import (
	fscmds "github.com/djdv/go-filesystem-utils/cmd"
	"github.com/djdv/go-filesystem-utils/cmd/parameters"
)

type Settings struct {
	fscmds.Settings
}

func (*Settings) Parameters() parameters.Parameters {
	return (*fscmds.Settings)(nil).Parameters()
}
