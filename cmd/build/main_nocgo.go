//go:build !cgo

package main

func setupEnv() envDeferFunc { return func() error { return nil } }
