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

func kindToCmdsOptionMaker(kind reflect.Kind) MakeOptionFunc {
	switch kind {
	case reflect.Bool:
		return cmds.BoolOption
	case reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32:
		return cmds.IntOption
	case reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32:
		return cmds.UintOption
	case reflect.Int64:
		return cmds.Int64Option
	case reflect.Uint64:
		return cmds.Uint64Option
	case reflect.Float32,
		reflect.Float64:
		return cmds.FloatOption
	case reflect.Complex64,
		reflect.Complex128:
		return cmds.StringOption
	case reflect.String:
		return cmds.StringOption
	case reflect.Array,
		reflect.Slice:
		return func(optionArgs ...string) cmds.Option {
			return cmds.DelimitedStringsOption(",", optionArgs...)
		}
	default:
		return nil
	}
}
