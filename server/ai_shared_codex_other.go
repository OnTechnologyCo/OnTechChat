//go:build !windows

package server

func sharedCodexExecutable() string            { return "" }
func sharedCodexHome(executable string) string { return "" }
