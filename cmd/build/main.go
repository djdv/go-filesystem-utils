// Command build attempts to build the fs command.
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

type envDeferFunc func() error

func main() {
	log.SetFlags(log.Lshortfile)
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal("Could not get working directory:", err)
	}
	var (
		pkgGoPath = filepath.Join("cmd", "fs")
		pkgFSPath = filepath.Join(cwd, pkgGoPath)
	)
	if _, err := os.Stat(pkgFSPath); err != nil {
		log.Fatal("Could not access pkg directory:", err)
	}
	restoreEnv := setupEnv()
	defer func() {
		if err := restoreEnv(); err != nil {
			log.Println(err)
		}
	}()

	const (
		goBin       = "go"
		goBuild     = "build"
		linkerFlags = "-ldflags=-s -w"
	)
	goArgs := []string{goBuild, linkerFlags, pkgFSPath}
	output, err := exec.Command(goBin, goArgs...).CombinedOutput()
	if err != nil {
		log.Printf("failed to run build command: %s\n%s",
			err, output,
		)
		os.Exit(1)
	}
	fmt.Fprint(os.Stdout, string(output))
}
