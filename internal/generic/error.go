package generic

import (
	"errors"
	"io"
)

type ConstError string

func (errStr ConstError) Error() string { return string(errStr) }

// CloseWithError closes all its arguments in order,
// and joins all errors (if any).
func CloseWithError(err error, closers ...io.Closer) error {
	var errs []error
	for _, closer := range closers {
		if cErr := closer.Close(); cErr != nil {
			errs = append(errs, cErr)
		}
	}
	if errs == nil {
		return err
	}
	errs = append([]error{err}, errs...)
	return errors.Join(errs...)
}
