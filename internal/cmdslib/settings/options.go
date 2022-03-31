package settings

import (
	"reflect"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings/options"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

func MakeOptions[settings any](opts ...options.CmdsOptionOption) []cmds.Option {
	return options.MustMakeCmdsOptions[Settings](append(optionMakers(), opts...)...)
	// return options.MustMakeCmdsOptions(empty, append(optionMakers(), options...)...)
}

func optionMakers() []options.CmdsOptionOption {
	var (
		makers = []options.OptionMaker{
			{
				Type:           reflect.TypeOf((*multiaddr.Multiaddr)(nil)).Elem(),
				MakeOptionFunc: cmds.StringOption,
			},
			{
				Type:           reflect.TypeOf((*time.Duration)(nil)).Elem(),
				MakeOptionFunc: cmds.StringOption,
			},
			{
				Type:           reflect.TypeOf((*filesystem.ID)(nil)).Elem(),
				MakeOptionFunc: cmds.StringOption,
			},
			{
				Type:           reflect.TypeOf((*filesystem.API)(nil)).Elem(),
				MakeOptionFunc: cmds.StringOption,
			},
		}
		opts = make([]options.CmdsOptionOption, len(makers))
	)
	for i, maker := range makers {
		opts[i] = options.WithMaker(maker)
	}
	return opts
}
