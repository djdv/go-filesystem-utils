// Code generated by "stringer -type=SourceID -linecomment"; DO NOT EDIT.

package parameters

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[CommandLine-1]
	_ = x[Environment-2]
}

const _SourceID_name = "command-linePROCESS_ENVIRONMENT"

var _SourceID_index = [...]uint8{0, 12, 31}

func (i SourceID) String() string {
	i -= 1
	if i >= SourceID(len(_SourceID_index)-1) {
		return "SourceID(" + strconv.FormatInt(int64(i+1), 10) + ")"
	}
	return _SourceID_name[_SourceID_index[i]:_SourceID_index[i+1]]
}
