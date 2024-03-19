package commands

import (
	"fmt"
	"strings"
)

// heading transforms the input so that
// the flag's text renderer interprets it
// as a text heading.
func heading(text string) string {
	return "# " + text
}

// underline transforms the input so that
// the flag's text renderer interprets it
// as a text to be underlined.
func underline(text string) string {
	return fmt.Sprintf(
		"%s\n%s",
		text,
		strings.Repeat("-", len(text)),
	)
}
