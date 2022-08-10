package main

import (
	"errors"
	"os"
	"os/exec"
	"testing"
)

const exitcodeParam = "exit-code-test"

func TestMain(m *testing.M) {
	if len(os.Args) >= 2 &&
		os.Args[1] == exitcodeParam {
		os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
		main()
		os.Exit(success)
	}
	os.Exit(m.Run())
}

func TestMainExit(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		args     []string
		expected int
	}{
		{
			"no args",
			nil,
			misuse,
		},
		{
			"help flag",
			[]string{"-help"},
			misuse,
		},
	} {
		var (
			name = test.name
			args = test.args
			want = test.expected
		)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var (
				execArgs = append([]string{exitcodeParam}, args...)
				cmd      = exec.Command(os.Args[0], execArgs...)
				err      = cmd.Run()
				exitErr  *exec.ExitError
			)
			if err == nil {
				t.Error("expected process to exit with error (but did not)")
			}
			if !errors.As(err, &exitErr) {
				t.Error("expected error's type to be ExitError (but isn't)")
			}
			if got := exitErr.ExitCode(); got != want {
				t.Errorf("error code mismatch"+
					"\n\tgot: %v"+
					"\n\twant: %v",
					got, want,
				)
			}
		})
	}
}
