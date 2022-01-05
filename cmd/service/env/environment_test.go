package serviceenv_test

import (
	"testing"

	serviceenv "github.com/djdv/go-filesystem-utils/cmd/service/env"
)

func TestAssert(t *testing.T) {
	env := serviceenv.MakeEnvironment()

	if _, err := serviceenv.Assert(env); err != nil {
		t.Fatal(err)
	}

	if _, err := serviceenv.Assert(nil); err == nil {
		t.Fatal("expected assert to error (nil input), but got nil error")
	}
}
