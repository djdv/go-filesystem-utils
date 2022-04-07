package settings

import (
	"fmt"
	"io"

	cmds "github.com/ipfs/go-ipfs-cmds"
)

var (
	Encoders = cmds.EncoderMap{
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
