//go:build !windows

package server

import "errors"

func runWindowsService(Config) error { return errors.New("--service is only supported on Windows") }
