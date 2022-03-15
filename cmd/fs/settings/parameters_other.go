//go:build !windows && !solaris && !linux && !darwin
// +build !windows,!solaris,!linux,!darwin

package settings

type PlatformSettings = POSIXSettings
