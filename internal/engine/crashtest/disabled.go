//go:build !integration

// Package crashtest provides a no-op barrier in ordinary builds.
package crashtest

// At does nothing outside integration builds.
func At(string) {}
