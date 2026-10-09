//go:build !windows

package server

import (
	"os"
	"path/filepath"
	"syscall"
)

func secureDirectory(p string) error { return os.Chmod(p, 0700) }
func lockDirectory(dir string) (*os.File, error) {
	f, e := os.OpenFile(filepath.Join(dir, "server.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}
func unlockDirectory(f *os.File) error {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return f.Close()
}
func syncDirectory(p string) {
	if f, e := os.Open(p); e == nil {
		_ = f.Sync()
		f.Close()
	}
}
