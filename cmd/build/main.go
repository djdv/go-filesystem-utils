package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func main() {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal("Could not get working directory:", err)
	}
	var (
		pkgGoPath = filepath.Join("cmd", "fs")
		pkgFSPath = filepath.Join(wd, pkgGoPath)
	)
	if _, err := os.Stat(pkgFSPath); err != nil {
		log.Fatal("Could not access pkg directory:", err)
	}

	if runtime.GOOS == "windows" {
		var (
			// If we have a C compiler, try to use it.
			haveCCompiler, useCgo bool
			// 2021.08.10 - Currently only GCC is supported on Windows
			cCompilers = []string{"gcc"} // Future: "clang", "msvc"
		)

		for _, compiler := range cCompilers {
			if _, err := exec.LookPath(compiler); err == nil {
				haveCCompiler = true
				break
			}
		}

		if haveCCompiler {
			var (
				progPath    = os.Getenv("ProgramFiles(x86)")
				fuseLibPath = filepath.Join(progPath, "WinFsp", "inc", "fuse")
			)
			// If we have the required headers, we can proceed with CGO.
			if _, err := os.Stat(fuseLibPath); err == nil {
				os.Setenv("CPATH", fuseLibPath)
				useCgo = true
			}
		}
		if !useCgo {
			os.Setenv("CGO_ENABLED", "0")
		}
	}

	output, err := exec.Command("go", "build", pkgFSPath).CombinedOutput()
	if err != nil {
		log.Fatalf("failed to run build command: %s\n%s",
			err, output,
		)
	}
	fmt.Printf("%s", output)
}
