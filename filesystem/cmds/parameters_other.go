//go:build !windows && !solaris && !linux && !darwin
// +build !windows,!solaris,!linux,!darwin

package filesystem

type PlatformSettings = POSIXSettings
