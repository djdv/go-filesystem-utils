// Command build attempts to build the fs command.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/djdv/go-filesystem-utils/internal/generic"
)

//go:generate stringer -type=buildMode
type buildMode int

const (
	regular buildMode = iota
	release
	debug
)

func main() {
	log.SetFlags(log.Lshortfile)
	buildMode, tags, output := parseFlags()
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal("could not get working directory:", err)
	}
	const (
		commandRoot   = "cmd"
		targetCommand = "fs"
	)
	var (
		pkgGoPath = filepath.Join(commandRoot, targetCommand)
		pkgFSPath = filepath.Join(cwd, pkgGoPath)
	)
	if _, err := os.Stat(pkgFSPath); err != nil {
		log.Fatal("could not access pkg directory:", err)
	}
	const (
		goBin   = "go"
		goBuild = "build"
		maxArgs = 5
	)
	goArgs := make([]string, 1, maxArgs)
	goArgs[0] = goBuild
	switch buildMode {
	case debug:
		const compilerDebugFlags = `-gcflags=all=-N -l`
		goArgs = append(goArgs, compilerDebugFlags)
	case release:
		const (
			buildTrimFlag      = `-trimpath`
			linkerReleaseFlags = `-ldflags=-s -w`
		)
		goArgs = append(goArgs, buildTrimFlag, linkerReleaseFlags)
	}
	if tags != "" {
		goArgs = append(goArgs, "-tags="+tags)
	}
	if output != "" {
		goArgs = append(goArgs, "-o="+output)
	}
	goArgs = append(goArgs, pkgFSPath)
	cmd := exec.Command(goBin, goArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	buildEnvironment, err := setupEnvironment(cmd.Environ())
	if err != nil {
		log.Fatal("could not setup process environment:", err)
	}
	cmd.Env = buildEnvironment
	if err := cmd.Run(); err != nil {
		log.Fatalf("failed to run build command: %s", err)
	}
}

func parseFlags() (mode buildMode, tags string, output string) {
	const (
		regularUsage = "standard go build with no compiler or linker flags when building"
		releaseUsage = "remove extra debugging data when building"
		debugUsage   = "disable optimizations when building"
		modeName     = "mode"
	)
	var (
		cmdName   = commandName()
		flagSet   = flag.NewFlagSet(cmdName, flag.ExitOnError)
		modeUsage = fmt.Sprintf(
			"%s\t- %s"+
				"\n%s\t- %s"+
				"\n%s\t- %s"+
				"\n\b",
			regular.String(), regularUsage,
			release.String(), releaseUsage,
			debug.String(), debugUsage,
		)
	)
	mode = release
	flagSet.Func(modeName, modeUsage, func(arg string) (err error) {
		mode, err = generic.ParseEnum(regular, debug, arg)
		return
	})
	flagSet.Lookup(modeName).DefValue = mode.String()
	const (
		tagName   = "tags"
		tagsUsage = "a comma-separated list of build tags" +
			"\nsupported in addition to Go's standard tags:" +
			"\nnofuse - build without FUSE host support" +
			"\nnoipfs - build without IPFS guest support" +
			"\nnonfs  - build without NFS host & guest support"
	)
	flagSet.StringVar(&tags, tagName, "", tagsUsage)
	const (
		outputName  = "o"
		outputUsage = "write the resulting executable" +
			" to the named output file or directory"
	)
	flagSet.StringVar(&output, outputName, "", outputUsage)
	if err := flagSet.Parse(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
	if args := flagSet.Args(); len(args) != 0 {
		var output strings.Builder
		flagSet.SetOutput(&output)
		flagSet.Usage()
		log.Fatalf("unexpected arguments: %s\n%s",
			strings.Join(args, ", "),
			output.String(),
		)
	}
	return
}

func commandName() string {
	execName := filepath.Base(os.Args[0])
	return strings.TrimSuffix(
		execName,
		filepath.Ext(execName),
	)
}
