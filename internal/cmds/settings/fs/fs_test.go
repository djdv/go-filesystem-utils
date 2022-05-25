package fs_test

import (
	"context"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/fs"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/options"
	"github.com/djdv/go-filesystem-utils/internal/cmds/settings/runtime"
	"github.com/djdv/go-filesystem-utils/internal/parameters"
)

func TestSettings(t *testing.T) {
	t.Parallel()
	t.Run("valid", testValid)
	t.Run("invalid", testInvalid)
}

func testValid(t *testing.T) {
	t.Parallel()
	fs.MustMakeOptions[*fs.Settings](options.WithBuiltin(true))
}

type invalidSettings bool

func (invalidSettings) Parameters(context.Context) parameters.Parameters { return nil }

func testInvalid(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected to panic due to \"%s\" but did not",
				runtime.ErrUnexpectedType.Error())
		}
	}()
	fs.MustMakeOptions[*invalidSettings]()
}
