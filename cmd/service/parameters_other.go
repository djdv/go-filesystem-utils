//go:build !windows && !solaris && !linux && !darwin
// +build !windows,!solaris,!linux,!darwin

package service

type PlatformSettings = POSIXSettings
