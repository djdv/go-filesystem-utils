package generic_test

import (
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/generic"
)

func channel(t *testing.T) {
	t.Parallel()
	t.Run("drain", drain)
}

func drain(t *testing.T) {
	t.Parallel()
	t.Run("valid", drainValid)
	t.Run("invalid", drainInvalid)
}

func drainValid(t *testing.T) {
	t.Parallel()
	buffered := make(chan int, 2)
	buffered <- 1
	buffered <- 2
	generic.DrainBuffer(buffered)
	const expected = 0
	if values := len(buffered); values != expected {
		t.Errorf("channel buffer was not drained"+
			"\n\tgot: %d"+
			"\n\twant: %d",
			values, expected)
	}
}

func drainInvalid(t *testing.T) {
	t.Parallel()
	unbuffered := make(chan int)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected to panic but did not" +
				" - unbuffered channel passed to drain")
		} else {
			t.Log("recovered from panic:\n\t", r)
		}
	}()
	generic.DrainBuffer(unbuffered)
}
