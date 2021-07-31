package main

import (
	"os"
	"testing"

	fscmds "github.com/djdv/go-filesystem-utils/cmd"
)

func TestMain(m *testing.M) {
	var (
		// Back up the test environment.
		testArgs = os.Args
		stdout   = os.Stdout
		stderr   = os.Stderr
		// Discard outputs.
		discard, err = os.OpenFile(os.DevNull, os.O_RDWR, 0)
	)
	if err != nil {
		panic(err)
	}

	// Mock a test environment.
	os.Stdout = discard
	os.Stderr = discard
	os.Args = []string{fscmds.ServiceName}

	// If main doesn't panic, we consider that a pass.
	main()
	os.Args = append(os.Args, "^__invalid_argument__^")
	main()

	// Restore the test environment.
	os.Args = testArgs
	os.Stdout = stdout
	os.Stderr = stderr

	os.Exit(m.Run())
}

// Does nothing other than suppress Go's testing warning
//`testing: warning: no tests to run`
func TestSuppressWarning(t *testing.T) {}
