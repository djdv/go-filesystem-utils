// Code generated by "stringer -type=Reason -linecomment"; DO NOT EDIT.

package environment

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[Canceled-1]
	_ = x[Idle-2]
	_ = x[Requested-3]
	_ = x[Error-4]
}

const _Reason_name = "request was canceledservice was idlestop was requestedruntime error caused stop to be called"

var _Reason_index = [...]uint8{0, 20, 36, 54, 92}

func (i Reason) String() string {
	i -= 1
	if i >= Reason(len(_Reason_index)-1) {
		return "Reason(" + strconv.FormatInt(int64(i+1), 10) + ")"
	}
	return _Reason_name[_Reason_index[i]:_Reason_index[i+1]]
}