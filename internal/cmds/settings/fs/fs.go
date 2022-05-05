// Package fs provides a `Settings` type and wrappers
// for the "fs" `Command` tree to use.
package fs

import (
	"context"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/options"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
	cmds "github.com/ipfs/go-ipfs-cmds"
)

// Settings is the root structure which contains
// any global settings that subcommands of `fs`
// must embed and check for.
type Settings struct{}

// Parameters returns the top level parameters for the Command "fs".
// All subcommands must relay these parameters (in addition to their own).
func (*Settings) Parameters(ctx context.Context) parameters.Parameters {
	out := make(chan parameters.Parameter)
	defer close(out)
	return out
}

// MustMakeOptions wraps MakeOptions,
// with a set of default options for Command "fs"
// (and its subcommands).
func MustMakeOptions[setPtr runtime.SettingsType[settings], settings any](opts ...options.ConstructorOption,
) []cmds.Option {
	cmdsOpts, err := options.MakeOptions[setPtr](opts...)
	if err != nil {
		panic(err)
	}
	return cmdsOpts
}
