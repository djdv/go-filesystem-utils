package settings

import (
	"context"
	"reflect"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings/arguments"
	"github.com/djdv/go-filesystem-utils/internal/cmdslib/settings/environment"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

// func ParseAll[settings any](ctx context.Context,
func ParseAll[settings any, setIntf cmdslib.SettingsConstraint[settings]](ctx context.Context,
	request *cmds.Request,
) (*settings, error) {
	var (
		typeHandlers = handlers()
		sources      = []cmdslib.SetFunc{
			arguments.SettingsFromCmds(request),
			environment.SettingsFromEnvironment(),
		}
	)
	return cmdslib.Parse[settings, setIntf](ctx, sources, typeHandlers...)
}

// TODO: Name.
func handlers() []cmdslib.TypeParser {
	return []cmdslib.TypeParser{
		{
			Type: reflect.TypeOf((*multiaddr.Multiaddr)(nil)).Elem(),
			ParseFunc: func(argument string) (interface{}, error) {
				return multiaddr.NewMultiaddr(argument)
			},
		},
		{
			Type: reflect.TypeOf((*time.Duration)(nil)).Elem(),
			ParseFunc: func(argument string) (interface{}, error) {
				return time.ParseDuration(argument)
			},
		},
		{
			Type: reflect.TypeOf((*filesystem.ID)(nil)).Elem(),
			ParseFunc: func(argument string) (interface{}, error) {
				return filesystem.StringToID(argument)
			},
		},
		{
			Type: reflect.TypeOf((*filesystem.API)(nil)).Elem(),
			ParseFunc: func(argument string) (interface{}, error) {
				return filesystem.StringToAPI(argument)
			},
		},
	}
}
