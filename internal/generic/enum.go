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

func ParseEnum[e Enum](start, end e, s string) (e, error) {
	normalized := strings.ToLower(s)
	for enum := start; enum <= end; enum++ {
		strVal := strings.ToLower(enum.String())
		if normalized == strVal {
			return enum, nil
		}
	}
	valids := make([]string, end)
	for i, sl := 0, start; sl <= end; i, sl = i+1, sl+1 {
		valids[i] = fmt.Sprintf(`"%s"`, sl.String())
	}
	return start, fmt.Errorf(
		`invalid Enum: "%s", want one of: %s`,
		s, strings.Join(valids, ", "),
	)
}
