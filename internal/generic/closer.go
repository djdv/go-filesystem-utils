package generic

// Closer wraps a close function
// to satisfy [io.Closer].
type Closer func() error

func (close Closer) Close() error { return close() }
