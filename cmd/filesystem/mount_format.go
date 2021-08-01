package fscmds

import (
	"context"
	"fmt"
	"io"

	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs-cmds/cli"
	"github.com/ipfs/go-ipfs/filesystem/manager"
	"github.com/ipfs/go-ipfs/filesystem/manager/errors"
)

type (
	console interface {
		io.Writer
		Print(string) error
	}

	emitter interface {
		Emit(interface{}) error
	}
	cmdsEmitFunc func(interface{}) error
)

// optionalOutputs has methods which may silently discard input if the request's options don't want them.
// e.g. if the requested client is a console, but the requested encoding type is not `Text`
// we'd provide an interface which discards console text,
// and an `Emit` method which sends values to an encoder
// which matching the requested type.
type optionalOutputs struct {
	console io.Writer
	emit    cmdsEmitFunc
}

func (oo *optionalOutputs) Print(s string) (err error) { _, err = oo.console.Write([]byte(s)); return }
func (oo *optionalOutputs) Emit(v interface{}) error   { return oo.emit(v) }

func makeOptionalOutputs(request *cmds.Request, re cmds.ResponseEmitter) optionalOutputs {
	var (
		outs               optionalOutputs
		console, isConsole = re.(cli.ResponseEmitter)
		encType            = cmds.GetEncoding(request, "")
		decorate           = isConsole && encType == cmds.Text
	)
	if decorate {
		// let console formatters write directly to the console
		// emitter discards input
		outs.console = console.Stdout()
		outs.emit = func(interface{}) error { return nil }
	} else {
		// otherwise the emitter provided to use gains exclusive access
		// to the output stream (in our scope).
		outs.console = io.Discard
		outs.emit = re.Emit
	}
	return outs
}

func flattenErrors(prefix string, errs []error) (err error) {
	for i, e := range errs {
		switch i {
		case 0:
			err = fmt.Errorf("%s encountered an error: %w", prefix, e)
		case 1:
			err = fmt.Errorf("%s encountered errors: %w; %s", prefix, errs[0], e)
		default:
			err = fmt.Errorf("%w; %s", err, e)
		}
	}
	return
}

// responseToResponses transforms cmds.Response values back into their original manager.Response form.
func responseToResponses(ctx context.Context, response cmds.Response) (manager.Responses, errors.Stream) {
	var (
		responses  = make(chan manager.Response)
		cmdsErrors = make(chan error)
	)
	go func() {
		defer close(responses)
		defer close(cmdsErrors)
		for {
			untypedResponse, err := response.Next()
			if err != nil {
				if err != io.EOF {
					select {
					case cmdsErrors <- err:
					case <-ctx.Done():
					}
				}
				return
			}

			// NOTE: Next is not guaranteed to return the exact type passed to `Emit`
			// local -> local responses (like from a chan emitter)
			// are typically concrete copies directly from `Emit`,
			// with remote -> local responses (like from an http emitter)
			// are typically pointers.
			var response manager.Response
			switch v := untypedResponse.(type) {
			case manager.Response:
				response = v
			case *manager.Response:
				response = *v
			default:
				select {
				case cmdsErrors <- cmds.Errorf(cmds.ErrImplementation,
					"emitter sent unexpected type+value: %#v", untypedResponse):
					continue
				case <-ctx.Done():
					return
				}
			}

			select {
			case responses <- response:
			case <-ctx.Done():
			}
		}
	}()
	return responses, cmdsErrors
}
