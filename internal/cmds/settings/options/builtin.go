package options

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

// constructorForKind accepts a builtin Go Kind and returns an `Option` constructor
// (or nil if unexpected Kind).
func constructorForKind(kind reflect.Kind) (constructor NewOptionFunc) {
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
