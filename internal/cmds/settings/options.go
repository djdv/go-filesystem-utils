package settings

import (
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/options"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// TODO: docs - in the future this will define and pass a set of type handlers
// right now it's just a redundant wrapper.
func MakeOptions[settings any,
	setPtr runtime.SettingsConstraint[settings]](opts ...options.ConstructorOption,
) []cmds.Option {
	return options.MustMakeCmdsOptions[setPtr](opts...)
}
