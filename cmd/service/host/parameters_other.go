//go:build !windows && !solaris && !linux && !darwin
// +build !windows,!solaris,!linux,!darwin

package host

type PlatformSettings = POSIXSettings
