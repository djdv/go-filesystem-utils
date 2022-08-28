//go:build cgo

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func setupEnv() envDeferFunc {
	if runtime.GOOS == "windows" {
		return setupCEnvWin()
	}
	return func() error { return nil }
}

func setupCEnvWin() envDeferFunc {
	const (
		x64SearchPath = "ProgramFiles(x86)"
		x86SearchPath = "ProgramFiles"
	)
	progPath, ok := os.LookupEnv(x64SearchPath)
	if !ok {
		progPath = os.Getenv(x86SearchPath)
	}
	fuseLibPath := filepath.Join(progPath, "WinFsp", "inc", "fuse")
	if _, err := os.Stat(fuseLibPath); err == nil {
		return setupCpathWinFSP(fuseLibPath)
	}
	return func() error { return nil }
}

func setupCpathWinFSP(fuseInc string) envDeferFunc {
	const compilerLibPathsKey = "CPATH"
	cpath, ok := os.LookupEnv(compilerLibPathsKey)
	if ok {
		libPaths := strings.Split(cpath, string(os.PathListSeparator))
		for _, path := range libPaths {
			if path == fuseInc {
				return func() error { return nil }
			}
		}
		appendedCpath := fmt.Sprint(cpath, os.PathListSeparator, fuseInc)
		if err := os.Setenv(compilerLibPathsKey, appendedCpath); err != nil {
			panic(err)
		}
		return func() error { return os.Setenv(compilerLibPathsKey, cpath) }
	}
	if err := os.Setenv(compilerLibPathsKey, fuseInc); err != nil {
		panic(err)
	}
	return func() error { return os.Unsetenv(compilerLibPathsKey) }
}

/* TODO: [lint] we still don't need this yet.
func haveCompiler() bool {
	// 2021.08.10 - Currently only GCC is supported on Windows
	cCompilers := []string{"gcc"} // Future: "clang", "msvc"
	for _, compiler := range cCompilers {
		if _, err := exec.LookPath(compiler); err == nil {
			return true
		}
	}
}
*/
