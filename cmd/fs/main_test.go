package main

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	testMain()
	os.Exit(m.Run())
}

func testMain() {
	var (
		argv              = os.Args
		stdout, stderr    = os.Stdout, os.Stderr
		discardWriter     = newDiscardWriter()
		restoreProgramEnv = func() {
			os.Args = argv
			os.Stdout = stdout
			os.Stderr = stderr

			if err := discardWriter.Close(); err != nil {
				panic(err)
			}
		}
	)
	defer restoreProgramEnv()

	// Don't output to `go test`'s streams.
	os.Stdout = discardWriter
	os.Stderr = discardWriter

	// Check that main doesn't panic.
	os.Args = []string{os.Args[0]}
	main()

	// An error will be returned to main+stderr, but still shouldn't panic.
	os.Args = append(os.Args, "^__invalid_argument__^")
	main()
}

func newDiscardWriter() *os.File {
	discard, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		panic(err)
	}
	return discard
}
