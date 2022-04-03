package settings

import (
	"reflect"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings/options"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings/runtime"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

func MakeOptions[settings any,
	setPtr runtime.SettingsConstraint[settings]](opts ...options.CmdsOptionOption,
) []cmds.Option {
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
		makerOpts = func() []options.CmdsOptionOption {
			makerOpts := make([]options.CmdsOptionOption, len(makers))
			for i, maker := range makers {
				makerOpts[i] = options.WithMaker(maker)
			}
			return makerOpts
		}()
	)

	opts = append(makerOpts, opts...)
	return options.MustMakeCmdsOptions[settings, setPtr](opts...)
}
