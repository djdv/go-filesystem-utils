//go:build !windows && !solaris && !linux && !darwin
// +build !windows,!solaris,!linux,!darwin

package ipc

type PlatformSettings = POSIXSettings
