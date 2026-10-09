//go:build !windows

package server

func nativeWindowsCLIProfiles() []windowsProfileEntry  { return nil }
func nativeWindowsCLIUserEnv(string) map[string]string { return map[string]string{} }
