package p9

import (
	"bufio"
	"bytes"
	"strings"
)

type (
	// AttributeValue is a tuple which should always
	// contain an attribute, and may contain a value.
	// Indexable via the constants [attribute] and [value].
	attributeValue [2]string
	pairs          []attributeValue
)

const (
	attribute = iota
	value
)

func (av attributeValue) attributeOnly() bool { return av[value] == "" }

// tokenize is for parsing data from Read/Write.
// Returning a list of tokens
// which contain a list of fields.
func tokenize(p []byte) pairs {
	var (
		reader  = bytes.NewReader(p)
		scanner = bufio.NewScanner(reader)
		pairs   pairs
	)
	for scanner.Scan() {
		pairs = append(
			pairs,
			splitPair(scanner.Text()),
		)
	}
	return pairs
}

func splitPair(pair string) attributeValue {
	const (
		left       = 0
		right      = 1
		fieldCount = 2
		mostCommon = fieldCount
		splitRune  = ' '
	)
	var (
		fields = split(
			make([]string, 0, mostCommon),
			pair, splitRune,
		)
		attrValue attributeValue
	)
	attrValue[attribute] = fields[left]
	if len(fields) == fieldCount {
		attrValue[value] = fields[right]
	}
	return attrValue
}

// split is like some of the functions within the
// [strings] pkg, but considers quotes and guarantees
// scan order.
func split(accumulator []string, input string, split rune) []string {
	const quote = '"'
	var (
		buffer strings.Builder
		quoted bool
	)
	for _, r := range input {
		switch {
		case r == quote:
			quoted = !quoted
		case !quoted && r == split:
			accumulator = append(accumulator, buffer.String())
			buffer.Reset()
		default:
			buffer.WriteRune(r)
		}
	}
	if buffer.Len() > 0 {
		accumulator = append(accumulator, buffer.String())
	}
	return accumulator
}
