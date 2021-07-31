package fscmds_test

import (
	"errors"
	"testing"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
)

func TestUtilities(t *testing.T) {
	t.Run("find server", func(t *testing.T) {
		srv, err := fscmds.FindLocalServer()
		if err == nil {
			t.Error("active server was found when not expected: ", srv)
		}
		expectedErr := fscmds.ErrServiceNotFound
		if !errors.Is(err, expectedErr) {
			t.Errorf("unexpected error"+
				"\n\twanted: %v"+
				"\n\tgot:%v",
				expectedErr, err)
		}
	})
}
