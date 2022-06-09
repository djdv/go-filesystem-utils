package option

import (
	"reflect"

	cmds "github.com/ipfs/go-ipfs-cmds"
)

func builtinOptions() []cmds.Option {
	return []cmds.Option{
		cmds.OptionEncodingType,
		cmds.OptionTimeout,
		cmds.OptionStreamChannels,
		cmds.BoolOption(cmds.OptLongHelp, "Show the full command help text."),
		cmds.BoolOption(cmds.OptShortHelp, "Show a short version of the command help text."),
	}
}

// newCmdsOptionFunc should follow the same conventions
// as `Option` constructors from the cmdslib pkg.
//
// I.e. the first argument is the primary name (e.g. `some name` => `--some-name`),
// additional arguments are aliases (`n` => `-n`),
// and the final argument is the description for the option (used in user facing help text).
type newCmdsOptionFunc func(...string) cmds.Option

// constructorForKind accepts a builtin Go Kind and returns an `Option` constructor
// (or nil if unexpected Kind).
func constructorForKind(kind reflect.Kind) (constructor newCmdsOptionFunc) {
	switch kind {
	case reflect.Bool:
		constructor = cmds.BoolOption
	case reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32:
		constructor = cmds.IntOption
	case reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32:
		constructor = cmds.UintOption
	case reflect.Int64:
		constructor = cmds.Int64Option
	case reflect.Uint64:
		constructor = cmds.Uint64Option
	case reflect.Float32,
		reflect.Float64:
		constructor = cmds.FloatOption
	case reflect.Complex64,
		reflect.Complex128:
		constructor = cmds.StringOption
	case reflect.String:
		constructor = cmds.StringOption
	case reflect.Array,
		reflect.Slice:
		constructor = func(optionArgs ...string) cmds.Option {
			return cmds.DelimitedStringsOption(",", optionArgs...)
		}
	}
	return
}
