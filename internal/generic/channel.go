// Package generic provides a set of type agnostic helpers.
package generic

// DrainBuffer removes any values
// from the buffered channel.
// If an unbuffered channel is passed, DrainBuffer panics.
// It is the callers responsibility to assure that
// the channel is not in use while draining.
func DrainBuffer[T any](ch <-chan T) {
	if cap(ch) == 0 {
		panic("unbuffered channel passed to drain")
	}
	for i := len(ch); i != 0; i-- {
		<-ch
	}
}
