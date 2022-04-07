package settings

import (
	"reflect"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/options"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

func MakeOptions[settings any,
	setPtr runtime.SettingsConstraint[settings]](opts ...options.ConstructorOption,
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
		makerOpts = func() []options.ConstructorOption {
			makerOpts := make([]options.ConstructorOption, len(makers))
			for i, maker := range makers {
				makerOpts[i] = options.WithMaker(maker)
			}
			return makerOpts
		}()
	)

	opts = append(makerOpts, opts...)
	return options.MustMakeCmdsOptions[setPtr](opts...)
}
