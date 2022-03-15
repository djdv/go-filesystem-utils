package main

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	var (
		// Back up the environment.
		originalArgs = os.Args
		stdout       = os.Stdout
		stderr       = os.Stderr
	)

	discard, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		panic(err)
	}

	os.Stdout = discard
	os.Stderr = discard
	os.Args = []string{os.Args[0]}

	// We're just making sure main doesn't panic.
	main()
	os.Args = append(os.Args, "^__invalid_argument__^")
	main()

	// Restore the environment.
	os.Args = originalArgs
	os.Stdout = stdout
	os.Stderr = stderr

	// Run any tests in this package.
	os.Exit(m.Run())
}

// Despite counting towards coverage, `TestMain` isn't considered
// a proper test according to Go. So the `go` tool emits a warning:
// `testing: warning: no tests to run`.
// We have this test which does nothing just to suppress that warning  ┐('～`；)┌
// func TestSuppressWarning(t *testing.T) {}
