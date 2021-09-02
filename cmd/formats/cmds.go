package formats

import (
	"fmt"
	"io"

	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs-cmds/cli"
)

type (
	// OptionalOutputs has methods which may silently discard input if the request's options don't want them.
	// e.g. if the requested client is a console, but the requested encoding type is not `Text`
	// we'd provide an interface which discards console text,
	// and an `Emit` method which sends values to an encoder
	// which matching the requested type.
	OptionalOutputs interface {
		Emit(v interface{}) error
		Print(s string) (err error)
		Console() io.Writer
	}
	optionalOutputs struct {
		emit    cmdsEmitFunc
		console io.Writer
	}

	cmdsEmitFunc func(interface{}) error
)

func (oo *optionalOutputs) Emit(v interface{}) error   { return oo.emit(v) }
func (oo *optionalOutputs) Print(s string) (err error) { _, err = oo.console.Write([]byte(s)); return }
func (oo *optionalOutputs) Console() io.Writer         { return oo.console }

// TODO: Name.
func MakeOptionalOutputs(request *cmds.Request, re cmds.ResponseEmitter) OptionalOutputs {
	var (
		outs               optionalOutputs
		console, isConsole = re.(cli.ResponseEmitter)
		encType            = cmds.GetEncoding(request, "")
		decorate           = isConsole && encType == cmds.Text
	)

	if decorate {
		// let console formatters write directly to the console,
		// emitter will discard input
		outs.console = console.Stdout()
		outs.emit = func(interface{}) error { return nil }
	} else {
		// otherwise give emitter passed to us, exclusive access
		// to the value stream.
		// discard decoration text
		outs.console = io.Discard
		outs.emit = re.Emit
	}
	return &outs
}

var (
	CmdsEncoders = cmds.EncoderMap{
		cmds.XML:  cmds.Encoders[cmds.XML],
		cmds.JSON: cmds.Encoders[cmds.JSON],
		cmds.Text: func(*cmds.Request) func(io.Writer) cmds.Encoder {
			return func(w io.Writer) cmds.Encoder { return &textEncoder{w: w} }
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

	const unitSeparator = '\x1F'
	var prefix, postfix string
	if e.prefixUnit {
		prefix = string(unitSeparator)
	}
	if e.terminateUnit {
		postfix = "\n"
	}

	_, encodingErr := fmt.Fprintf(e.w, "%s%s%s", prefix, v, postfix)
	if encodingErr != nil {
		return encodingErr
	}

	if !e.prefixUnit &&
		!e.terminateUnit {
		// If no formatting rules were explicitly provided;
		// implicitly prefix subsequent values.
		// Otherwise there's no way to know where 1 unit ends and another begins.
		//
		// While `\x1F` is not `\x20`, it usually renders as a space.
		// So text should look like `unit1 unit2 unit3...` on a console,
		// while still being machine parser friendly.
		e.prefixUnit = true
	}
	return nil
}
