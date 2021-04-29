package formats

import (
	"io"

	"github.com/djdv/go-filesystem-utils/manager"
	"github.com/olekukonko/tablewriter"
)

//go:generate stringer -type=tableColumn -linecomment
type (
	tableColumn int
	tableHeader []string
)

const (
	thBinding tableColumn = iota // Binding
	thError                      // Error

	thEnd // End
)

// newTableFormatter constructs the interface
// used to render console formatted data tables,
// to the passed in writer.
/* e.g.
          Binding         │            Error
──────────────────────────┼───────────────────────────────
  /path/n/binding
  /dns4/localhost/tcp/564
  /dns4/localhost/tcp/564 │ ⚠️Example error: addr in use
  /dns4/localhost/tcp/897
*/
func newTableFormatter(writer io.Writer) (graphics *tablewriter.Table, header *tableHeader) {
	// Construct the table renderer
	// and bind it to the output writer.
	table := tablewriter.NewWriter(writer)

	// various frame decorations
	table.SetAutoFormatHeaders(false)
	table.SetAutoMergeCells(false)
	table.SetBorder(false)
	table.SetColumnSeparator("│")
	table.SetCenterSeparator("┼")
	table.SetRowSeparator("─")

	/* NOTE: autowrap is a nice feature, but currently breaks a variety of things
	if the line is wrapped by the tablewriter
		) table.NumLines is inaccurate :^(
			) breaks cursor movement / redraw
		) colors apply to line 0, but wrapped lines are always plaintext
	we may need to fix this library or choose a different one,
	or just have simpler and/or smaller output
	*/
	table.SetAutoWrapText(false)

	return table, new(tableHeader)
}

func decorateHeader(table *tablewriter.Table, header []string) {
	hColors := make([]tablewriter.Colors, len(header))
	for i := range hColors {
		hColors[i] = tablewriter.Colors{tablewriter.Bold}
	}
	table.SetHeaderColor(hColors...)
}

// addResponseAsRow will render the response as a row of cells,
// and insert that row into the table.
// addResponseAsRow may modify the table's header.
func addResponseAsRow(graphics *tablewriter.Table, currentHeader *tableHeader, resp manager.Response) {
	var (
		maddr         = resp.Request
		responseError = resp.Error

		rowWidth int

		maybeExtendHeaderTo = func(column tableColumn) {
			var (
				header      = *currentHeader
				currentLen  = len(header)
				columnIndex = int(column) + 1
			)
			if currentLen >= columnIndex {
				return
			}

			var (
				missingColumns = columnIndex - currentLen
				tail           = make(tableHeader, missingColumns)
				columnOffset   = tableColumn(currentLen)
			)
			for i := range tail {
				tail[i] = columnOffset.String()
				columnOffset++
			}

			// NOTE: graphics.SetX is actually more like graphics.AppendX
			// We can't reset these values. So only pass in the new cells.
			// (Otherwise we encounter a render bug: [old, old] vs [old, new])
			// TODO [Investigate]: This is probably not intended upstream,
			// and should likely work with the full slice.
			graphics.SetHeader(tail)
			decorateHeader(graphics, tail)

			// this change will persist through calls
			header = append(header, tail...)
			*currentHeader = header
		}
	)

	if responseError == nil {
		maybeExtendHeaderTo(thBinding)
		rowWidth = int(thBinding) + 1
	} else {
		maybeExtendHeaderTo(thError)
		rowWidth = int(thError) + 1
	}
	var (
		cells      = make([]string, rowWidth)
		cellColors = make([]tablewriter.Colors, rowWidth)
	)

	if maddr != nil {
		cells[thBinding] = maddr.String()
		cellColors[thBinding] = tablewriter.Colors{tablewriter.FgGreenColor}
	}
	if responseError != nil {
		cells[thError] = "⚠️" + resp.Error.Error()
		cellColors[thError] = tablewriter.Colors{tablewriter.FgRedColor}
	}

	graphics.Rich(cells, cellColors)
}
