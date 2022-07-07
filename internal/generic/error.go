package generic

type ConstError string

func (errStr ConstError) Error() string { return string(errStr) }
