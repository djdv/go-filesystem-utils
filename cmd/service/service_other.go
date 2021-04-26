//go:build !windows && !solaris && !linux && !darwin
// +build !windows,!solaris,!linux,!darwin

package service

var servicePlatformOptions = servicePosixOptions
