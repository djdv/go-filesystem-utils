package fscmds_test

import (
	"errors"
	"testing"

	"github.com/djdv/go-filesystem-utils/cmd/ipc"
)

func TestUtilities(t *testing.T) {
	t.Run("find server", func(t *testing.T) {
		srv, err := ipc.FindLocalServer()
		if err == nil {
			t.Error("active server was found when not expected: ", srv)
		}
		expectedErr := ipc.ErrServiceNotFound
		if !errors.Is(err, expectedErr) {
			t.Errorf("unexpected error"+
				"\n\twanted: %v"+
				"\n\tgot:%v",
				expectedErr, err)
		}
	})
}
