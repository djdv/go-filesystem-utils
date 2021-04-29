package formats

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/djdv/go-filesystem-utils/manager"
	"github.com/olekukonko/tablewriter"
)

func RenderResponseToOutputs(ctx context.Context, responses manager.Responses,
	outputs OptionalOutputs) <-chan error {
	var (
		errs                          = make(chan error)
		renderCtx, renderCancel       = context.WithCancel(ctx)
		consoleRenderer, renderErrors = responsesToConsole(renderCtx, outputs.Console())
	)
	go func() {
		defer renderCancel()
		defer close(errs)
		for {
			select {
			case response, ok := <-responses:
				if !ok {
					return
				}

				// emit structured data requests (JSON, XML, et al.)
				// to their formatter
				if emitErr := outputs.Emit(&response); emitErr != nil {
					errs <- emitErr
					return
				}

				select {
				case consoleRenderer <- response:
					renderErr := <-renderErrors
					if renderErr != nil {
						errs <- renderErr
						return
					}
				case <-ctx.Done():
					return
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	return errs
}

func EmitResponses(ctx context.Context, emit cmdsEmitFunc,
	responses manager.Responses) <-chan error {
	emitErrs := make(chan error, 1)
	go func() {
		defer close(emitErrs)
		for {
			select {
			case response, ok := <-responses:
				if !ok {
					return
				}
				err := emit(&response)
				if err != nil {
					// emitter encountered a fault
					// stop emitting values to its observer
					emit = func(interface{}) error { return nil }
					emitErrs <- err
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return emitErrs
}

// responsesToConsole sets up a pair of synchronous channels.
// The receiver receives a Response, and renders it to the console.
// The sender returns the error of the render operation, if any.
// The input context must be canceled when done sending.
func responsesToConsole(ctx context.Context, console io.Writer) (chan<- manager.Response, <-chan error) {
	var (
		responses             = make(chan manager.Response)
		renderErrs            = make(chan error)
		graphics, tableHeader = newTableFormatter(console)
		scrollBack            int
	)
	go func() {
		defer os.Stdout.Sync()
		defer close(responses)
		defer close(renderErrs)
		for {
			select {
			case response, ok := <-responses: // (re)render response to the console, as a formatted table
				if !ok {
					panic("caller closed input channel, should cancel input context")
				}
				// start drawing this many lines above the current line
				scrollBack = graphics.NumLines()
				renderErrs <- overdrawResponse(console, scrollBack,
					graphics, tableHeader, response)
			case <-ctx.Done():
				return
			}
		}
	}()
	return responses, renderErrs
}

// drawResponse modifies the response table,
// and renders the table to its outputs.
func drawResponse(graphics *tablewriter.Table, header *tableHeader, response manager.Response) {
	addResponseAsRow(graphics, header, response)
	// NOTE: This will blit
	// the entire table, not just the row.
	graphics.Render()
}

// overdrawResponse will render the response to its output,
// and adjust the console to draw over-top of previous renders / update in-place.
func overdrawResponse(console io.Writer, scrollBack int,
	graphics *tablewriter.Table, header *tableHeader, response manager.Response) error {
	const (
		headerHeight      = 2
		ansiHorizontal    = "\033[%dG"
		ansiUp            = "\033[%dA"
		terminalEscapeFmt = ansiHorizontal + ansiUp
	)
	if scrollBack != 0 {
		_, err := console.Write([]byte(
			fmt.Sprintf(terminalEscapeFmt,
				0,                       // go to the beginning of the line
				headerHeight+scrollBack, // and move cursor up N lines
			)))
		if err != nil {
			// e̵x̷p̶e̷c̸t̶ ̶b̶a̶d̷ ̶t̴e̵r̷m̵i̴n̴a̴l̷ ̵g̵r̸a̶p̸h̴i̵c̶s̷
			return fmt.Errorf("console control sequence - write error: %w", err)
		}
	}

	drawResponse(graphics, header, response)
	return nil
}
