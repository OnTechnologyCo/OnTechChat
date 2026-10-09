//go:build !windows

package server

import "os"

func replaceConfigFile(from, to string) error { return os.Rename(from, to) }
func rejectConfigReparse(string) error        { return nil }
