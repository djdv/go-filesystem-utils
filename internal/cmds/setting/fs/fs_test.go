package fs_test

import (
	"context"
	"testing"

	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/fs"
	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/option"
	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/runtime"
	"github.com/djdv/go-filesystem-utils/internal/parameter"
)

func TestSettings(t *testing.T) {
	t.Parallel()
	t.Run("valid", testValid)
	t.Run("invalid", testInvalid)
}

func testValid(t *testing.T) {
	t.Parallel()
	fs.MustMakeOptions[*fs.Settings](option.WithBuiltin(true))
}

type invalidSettings bool

func (invalidSettings) Parameters(context.Context) parameter.Parameters { return nil }

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
