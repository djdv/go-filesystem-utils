package generic

import "unsafe"

// ConvertString avoids the implicit copy that would be incurred by
// doing `[]byte(input)`.
// Caller should not modify the return value.
// (Modifying read-only memory /will/ segfault.)
func ConvertString(input string) []byte {
	char0 := unsafe.StringData(input)
	return unsafe.Slice(char0, len(input))
}

// ConvertBytes avoids the implicit copy that would be incurred by
// doing `string(input)`.
// Caller should not modify `input`.
// (Especially while the return value is being referenced.)
func ConvertBytes(input []byte) string {
	return unsafe.String(&input[0], len(input))
}
