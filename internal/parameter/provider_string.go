// Code generated by "stringer -type=Provider -linecomment"; DO NOT EDIT.

package parameter

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[CommandLine-1]
	_ = x[Environment-2]
}

const _Provider_name = "command-linePROCESS_ENVIRONMENT"

var _Provider_index = [...]uint8{0, 12, 31}

func (i Provider) String() string {
	i -= 1
	if i >= Provider(len(_Provider_index)-1) {
		return "Provider(" + strconv.FormatInt(int64(i+1), 10) + ")"
	}
	return _Provider_name[_Provider_index[i]:_Provider_index[i+1]]
}
