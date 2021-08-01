package fscmds

import (
	"context"
	goerrors "errors"
	"fmt"
	"io"
	"runtime"
	"strings"

	cmds "github.com/ipfs/go-ipfs-cmds"
	"github.com/ipfs/go-ipfs/filesystem"
	"github.com/ipfs/go-ipfs/filesystem/manager"
	"github.com/ipfs/go-ipfs/filesystem/manager/errors"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/olekukonko/tablewriter"
)

//go:generate stringer -type=tableColumn -linecomment -output formatter_string.go
type tableColumn int

const (
	thHAPI    tableColumn = iota // Host API
	thNAPI                       // Node API
	thBinding                    // Binding
	thExtra                      // Options

	// NOTE: rows must align to this width
	tableWidth
)

// constructs the interface used to draw graphical tables to a writer
func newTableFormatter(writer io.Writer) *tablewriter.Table {
	tableHeader := []string{ // construct the header cells
		thHAPI.String(),
		thNAPI.String(),
		thBinding.String(),
		thExtra.String(),
	}

	table := tablewriter.NewWriter(writer) // construct the table renderer
	table.SetHeader(tableHeader)           // insert header cells

	hColors := make([]tablewriter.Colors, tableWidth) // apply styles to them
	for i := range hColors {
		hColors[i] = tablewriter.Colors{tablewriter.Bold}
	}
	table.SetHeaderColor(hColors...)

	// various frame decorations
	table.SetAutoFormatHeaders(false)
	table.SetBorder(false)
	table.SetColumnSeparator("│")
	table.SetCenterSeparator("┼")
	table.SetRowSeparator("─")

	/* NOTE: autowrap is a nice feature, but currently breaks a variety of things
	if the line is wrapped by the tablewriter
		) table.NumLines is inaccurate :^(
			) breaks cursor movement / redraw
		) colors apply to line 0, but wrapped lines are always plaintext
	we may need to fix this library or choose a different one, or just have simpler output
	*/
	table.SetAutoWrapText(false)

	return table
}

// XXX: sloppy
func responseAsTableRow(resp manager.Response) ([]string, []tablewriter.Colors) {
	row := make([]string, tableWidth)
	maddr := resp.Request

	if maddr != nil { // retrieve row data from the multiaddr (if any)
		multiaddr.ForEach(maddr, func(comp multiaddr.Component) bool {
			proto := comp.Protocol()
			switch proto.Code {
			case int(filesystem.Fuse):
				row[thHAPI] = proto.Name
				row[thNAPI] = comp.Value()

			case int(filesystem.Plan9Protocol):
				row[thHAPI] = proto.Name
				row[thNAPI] = comp.Value()

				// XXX: quick 9P formatting hacks; make formal and break out of here
				_, tail := multiaddr.SplitFirst(maddr)       // strip fs header
				hopefullyNet, _ := multiaddr.SplitLast(tail) // strip path tail
				if hopefullyNet == nil {
					break
				}
				if addr, err := manet.ToNetAddr(hopefullyNet); err == nil {
					row[thExtra] = fmt.Sprintf("Listening on: %s://%s", addr.Network(), addr.String())
				}

			case int(filesystem.PathProtocol):
				localPath := comp.Value()
				if runtime.GOOS == "windows" { // `/C:\path` -> `C:\path`
					localPath = strings.TrimPrefix(localPath, `/`)
				}
				row[thBinding] = localPath
			}
			return true
		})
	}

	// create the corresponding color values for the table's row
	// XXX: non-deuteranopia friendly colours
	rowColors := make([]tablewriter.Colors, tableWidth)
	for i := range rowColors {
		switch {
		case resp.Error == nil:
			rowColors[i] = tablewriter.Colors{tablewriter.FgGreenColor}
			continue

			// TODO: do this the proper way
			// we need to check the string value in unmarshal, and check with Is here
		case goerrors.Is(resp.Error, errors.Unwound):
			rowColors[i] = tablewriter.Colors{tablewriter.FgYellowColor}
		default:
			rowColors[i] = tablewriter.Colors{tablewriter.FgRedColor}
		}
		row[thExtra] = "/!\\ " + resp.Error.Error()
		// TODO: pointer to header table
		// change "Options" to "Options/Errors" dynamically
	}

	return row, rowColors
}

// TODO: English
// responsesToConsole renders responses sent on the returned channel,
// to the supplied buffer (as terminal text data).
// Caller should cancel the context when done drawing.
func responsesToConsole(ctx context.Context, renderBuffer io.Writer) (chan<- manager.Response, errors.Stream) {
	var (
		responses  = make(chan manager.Response)
		renderErrs = make(chan error)
		graphics   = newTableFormatter(renderBuffer)
		scrollBack int
	)
	go func() {
		defer close(responses)
		defer close(renderErrs)
		for {
			select {
			case response, ok := <-responses: // (re)render response to the console, as a formatted table
				if !ok {
					panic("caller closed input channel")
				}
				scrollBack = graphics.NumLines() // start drawing this many lines above the current line
				if err := overdrawResponse(renderBuffer, scrollBack, graphics, response); err != nil {
					select {
					case renderErrs <- err:
					case <-ctx.Done():
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return responses, renderErrs
}

func drawResponse(graphics *tablewriter.Table, response manager.Response) {
	graphics.Rich(responseAsTableRow(response)) // adds the row to the table
	graphics.Render()                           // draws the entire table
}

func overdrawResponse(console io.Writer, scrollBack int, graphics *tablewriter.Table, response manager.Response) (err error) {
	const headerHeight = 2
	if scrollBack != 0 {
		// TODO: this needs to be abstracted; byte sequence is going to depend on the terminal
		if _, err = console.Write([]byte(
			fmt.Sprintf("\033[%dA\033[%dG",
				headerHeight+scrollBack, // move cursor up N lines
				0,                       // and go to the beginning of the line
			))); err != nil {
			return
		}
	}
	// draw the table, at cursors current position
	// (this should draw over any characters from a previous call, if any)
	drawResponse(graphics, response)
	return
}

func renderToConsole(request *cmds.Request, output optionalOutputs, inputErrors errors.Stream, responses manager.Responses) []error {
	var (
		renderCtx, renderCancel       = context.WithCancel(request.Context)
		consoleRenderer, renderErrors = responsesToConsole(renderCtx, output.console)
		allErrs                       = errors.Merge(inputErrors, renderErrors)
		encounteredErrs               []error
	)

renderLoop: // TODO: simplify or extract or something, this seems worse than `emitResponses`'s
	for {
		select {
		case response, ok := <-responses:
			if !ok {
				break renderLoop
			}
			if emitErr := output.Emit(response); emitErr != nil {
				encounteredErrs = append(encounteredErrs, emitErr)
				break renderLoop
			}
			select {
			case consoleRenderer <- response:
			case anyErr := <-allErrs:
				encounteredErrs = append(encounteredErrs, anyErr)
				break renderLoop
			case <-renderCtx.Done():
				encounteredErrs = append(encounteredErrs, renderCtx.Err())
				break renderLoop
			}
		case <-renderCtx.Done():
			encounteredErrs = append(encounteredErrs, renderCtx.Err())
			break renderLoop
		}
	}
	renderCancel()

	remainingErrs := errors.Accumulate(request.Context, allErrs)
	if len(encounteredErrs) == 0 {
		encounteredErrs = remainingErrs
	} else {
		encounteredErrs = append(encounteredErrs, remainingErrs...)
	}
	return encounteredErrs
}
