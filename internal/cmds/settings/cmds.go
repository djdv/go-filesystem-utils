package settings

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/djdv/go-filesystem-utils/filesystem"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/arguments"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/environment"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/multiformats/go-multiaddr"
)

var (
	CmdsEncoders = cmds.EncoderMap{
		cmds.XML:  cmds.Encoders[cmds.XML],
		cmds.JSON: cmds.Encoders[cmds.JSON],
		cmds.Text: func(*cmds.Request) func(io.Writer) cmds.Encoder {
			return func(w io.Writer) cmds.Encoder {
				return &textEncoder{w: w, terminateUnit: true}
			}
		},
		cmds.TextNewline: func(*cmds.Request) func(io.Writer) cmds.Encoder {
			return func(w io.Writer) cmds.Encoder {
				return &textEncoder{w: w, terminateUnit: true}
			}
		},
	}

	errEmptyResponseRequest = fmt.Errorf("response's Request field must be populated")
)

type textEncoder struct {
	w                         io.Writer
	prefixUnit, terminateUnit bool
}

func (e *textEncoder) Encode(v interface{}) error {
	if v == nil {
		return errEmptyResponseRequest
	}

	// TODO: Write note about this. CLI.Run uses text instead of textnl unfortunately.
	// const unitSeparator = '\x1F'
	const unitSeparator = "\n"
	var prefix, postfix string
	if e.prefixUnit {
		prefix = unitSeparator
	}
	if e.terminateUnit {
		postfix = "\n"
	}

	_, encodingErr := fmt.Fprintf(e.w, "%s%v%s", prefix, v, postfix)
	if encodingErr != nil {
		return encodingErr
	}

	if !e.prefixUnit &&
		!e.terminateUnit {
		// TODO: this is not true anymore with the cli.Run workaround.
		// If no formatting rules were explicitly provided;
		// implicitly prefix subsequent values.
		// Otherwise there's no way to know where 1 unit ends and another begins.
		//
		// While `\x1F`(Unit separator)
		// is not `\x20`(Space), it usually renders as a space.
		// So text should look like `unit1 unit2 unit3...` on a console,
		// while still being machine parser friendly.
		e.prefixUnit = true
	}
	return nil
}

func Parse[settings any, setIntf runtime.SettingsConstraint[settings]](ctx context.Context,
	request *cmds.Request,
) (*settings, error) {
	var (
		typeHandlers = handlers()
		sources      = []runtime.SetFunc{
			arguments.SettingsFromCmds(request),
			environment.SettingsFromEnvironment(),
		}
	)
	return runtime.Parse[settings, setIntf](ctx, sources, typeHandlers...)
}

// TODO: Name.
func handlers() []runtime.TypeParser {
	return []runtime.TypeParser{
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
