//go:build cgo && !windows

package main

func setupEnvironment(environment []string) ([]string, error) { return environment, nil }
