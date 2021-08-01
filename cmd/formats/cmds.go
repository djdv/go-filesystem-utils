package formats

import (
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
