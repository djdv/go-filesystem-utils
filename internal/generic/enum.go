package generic

import (
	"fmt"
	"strings"

	"golang.org/x/exp/constraints"
)

type Enum interface {
	constraints.Integer
	fmt.Stringer
}

// TODO: this should be a constructor+method
// makeEnum(start, end) Enum; Enum.Parse(s)
func ParseEnum[e Enum](start, end e, s string) (e, error) {
	normalized := strings.ToLower(s)
	for enum := start + 1; enum != end; enum++ {
		strVal := strings.ToLower(enum.String())
		if normalized == strVal {
			return enum, nil
		}
	}
	return start, fmt.Errorf("invalid Enum: \"%s\"", s)
}
