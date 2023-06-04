//go:build cgo

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func setupEnvironment(environment []string) ([]string, error) {
	const librarySearchPathKey = "CPATH"
	searchPaths, searchPathsIndex, searchPathsSet := lookupEnv(environment, librarySearchPathKey)
	if searchPathsSet {
		if hasFuseHeader(searchPaths) {
			return environment, nil
		}
	}
	fuseLibPath, err := getDefaultFUSELibPath()
	if err != nil {
		return nil, err
	}
	if searchPathsSet {
		newSearchPaths := appendSearchPaths(searchPaths, fuseLibPath)
		setEnv(environment, searchPathsIndex, librarySearchPathKey, newSearchPaths)
	} else {
		environment = append(environment, newEnvPair(librarySearchPathKey, fuseLibPath))
	}
	return environment, nil
}

func lookupEnv(environment []string, key string) (string, int, bool) {
	for i, pair := range environment {
		if strings.HasPrefix(pair, key) {
			const separator = "="
			valueIndex := len(key) + len(separator)
			return pair[valueIndex:], i, true
		}
	}
	return "", -1, false
}

func hasFuseHeader(pathList string) bool {
	paths := strings.Split(pathList, string(os.PathListSeparator))
	const headerName = "fuse.h"
	for _, path := range paths {
		headerPath := filepath.Join(path, headerName)
		if _, err := os.Stat(headerPath); err == nil {
			return true
		}
	}
	return false
}

func getDefaultFUSELibPath() (string, error) {
	const findErrFmt = `could not find WinFSP's FUSE library "%s"`
	fuseLibPath := getFUSELibPath()
	if _, err := os.Stat(fuseLibPath); err != nil {
		return "", fmt.Errorf(findErrFmt+": %w", fuseLibPath, err)
	}
	if !hasFuseHeader(fuseLibPath) {
		return "", fmt.Errorf(findErrFmt, fuseLibPath)
	}
	return fuseLibPath, nil
}

func getFUSELibPath() string {
	return filepath.Join(
		getProgramsPath(),
		"WinFsp", "inc", "fuse",
	)
}

func getProgramsPath() string {
	const (
		x64SearchPath = "ProgramFiles(x86)"
		x86SearchPath = "ProgramFiles"
	)
	progPath, ok := os.LookupEnv(x64SearchPath)
	if ok {
		return progPath
	}
	return os.Getenv(x86SearchPath)
}

func appendSearchPaths(searchPaths, path string) string {
	return fmt.Sprintf("%s%c%s", searchPaths, os.PathListSeparator, path)
}

func setEnv(environment []string, index int, key, value string) {
	environment[index] = newEnvPair(key, value)
}

func newEnvPair(key, value string) string {
	const separator = "="
	return key + separator + value
}
