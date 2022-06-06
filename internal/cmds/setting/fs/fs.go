// Package fs provides a `Settings` type and wrappers
// that subcommands of "fs" must use.
package fs

import (
	"context"
	"reflect"
	"time"

	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/argument"
	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/environment"
	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/option"
	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/request"
	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/runtime"
	"github.com/djdv/go-filesystem-utils/internal/parameter"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// Settings is the root structure which contains
// any global settings that subcommands of Command "fs"
// must embed and check for.
type Settings struct{}

// Parameters returns the top level parameters for the Command "fs".
// All subcommands must relay these parameters (in addition to their own).
func (*Settings) Parameters(ctx context.Context) parameter.Parameters {
	out := make(chan parameter.Parameter)
	defer close(out)
	return out
}

// MustMakeOptions wraps MakeOptions,
// with a set of default options for Command "fs"
// (and its subcommands).
func MustMakeOptions[setPtr runtime.SettingsType[settings],
	settings any](opts ...option.ConstructorOption,
) []cmds.Option {
	cmdsOpts, err := option.MakeOptions[setPtr](opts...)
	if err != nil {
		panic(err)
	}
	return cmdsOpts
}

func Parse[setIntf runtime.SettingsType[settings], settings any](ctx context.Context,
	req *cmds.Request,
) (*settings, error) {
	var (
		typeHandlers = handlers()
		sources      = []argument.SetFunc{
			request.ValueSource(req),
			environment.ValueSource,
		}
	)
	return argument.Parse[setIntf](ctx, sources, typeHandlers...)
}

// TODO: Name.
func handlers() []argument.TypeParser {
	return []argument.TypeParser{
		{
			Type: reflect.TypeOf((*time.Duration)(nil)).Elem(),
			ParseFunc: func(argument string) (interface{}, error) {
				return time.ParseDuration(argument)
			},
		},
	}
}
