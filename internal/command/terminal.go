package command

import (
	"fmt"
	"os"
	"reflect"
	"unsafe"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/muesli/termenv"
)

func newRenderer() (*glamour.TermRenderer, error) {
	const (
		glamourStyleKey = `GLAMOUR_STYLE`
		optionsLength   = 3
	)
	var (
		glamourStyle  = os.Getenv(glamourStyleKey)
		haveEnvStyle  = glamourStyle != ""
		renderOptions = make([]glamour.TermRendererOption, optionsLength)
	)
	renderOptions[0] = glamour.WithWordWrap(0)
	renderOptions[1] = glamour.WithPreservedNewLines()
	if haveEnvStyle {
		renderOptions[2] = glamour.WithEnvironmentConfig()
	} else {
		renderOptions[2] = glamour.WithStyles(makeStyle())
	}
	// If this returns an error, the user
	// most likely has invalid `GLAMOUR_STYLE` value.
	renderer, err := glamour.NewTermRenderer(renderOptions...)
	if err != nil {
		const prefix = "could not initialize glamour renderer:"
		var msg string
		if haveEnvStyle {
			msg = fmt.Sprintf(
				"%s (`%s=%s`)",
				prefix, glamourStyleKey, glamourStyle,
			)
		} else {
			msg = prefix
		}
		return nil, fmt.Errorf(
			"%s %w",
			msg, err,
		)
	}
	return renderer, nil
}

func makeStyle() ansi.StyleConfig {
	const (
		cornflowerBlue = "#6495ED" // 256: ~69
		jet            = "#363636" // 256: ~237
	)
	var (
		style          ansi.StyleConfig
		codeFg, codeBg string
	)
	if termenv.HasDarkBackground() {
		style = glamour.DarkStyleConfig
		style.CodeBlock.Theme = "monokai"
		codeFg = cornflowerBlue
		codeBg = jet
	} else {
		style = glamour.LightStyleConfig
		style.CodeBlock.Theme = "monokailight"
		codeFg = cornflowerBlue
		codeBg = *style.Code.BackgroundColor
	}
	for _, block := range []*ansi.StyleBlock{
		&style.H1,
		&style.H2,
		&style.H3,
		&style.H4,
		&style.H5,
		&style.H6,
		&style.Code,
	} {
		// No padding.
		block.Prefix = ""
		block.Suffix = ""
	}
	// Remove baked preset chroma.
	// (Causes `.Theme` to be processed)
	style.CodeBlock.Chroma = nil
	// Override preset code block color.
	style.Code.Color = &codeFg
	style.Code.BackgroundColor = &codeBg
	// Assume operator's text color
	// is already what they want it to be.
	style.Document.Color = nil
	style.Text.Color = nil
	// Remove automatic line spacing.
	style.Document.BlockPrefix = ""
	style.Document.BlockSuffix = ""
	// No margins.
	var margin uint
	style.Document.Margin = &margin
	return style
}

func bold(text string) string {
	return "**" + text + "**"
}

func italic(text string) string {
	return "*" + text + "*"
}

// HACK:
// We need to read a color value from the renderers
// style sheet, but this is not exposed.
// (See: [newItalicUnderlineRenderer].)
func _extractStyle(renderer *glamour.TermRenderer) *ansi.StyleConfig {
	const fieldName = "ansiOptions"
	var ( // XXX: Circumventing the type system.
		options = reflect.ValueOf(renderer).
			Elem().
			FieldByName(fieldName)
		styles = reflect.NewAt(
			options.Type(),
			unsafe.Pointer(options.UnsafeAddr())).
			Elem().
			Interface().(ansi.Options).Styles
	)
	return &styles
}
