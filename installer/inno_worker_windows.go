//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
)

// Only Inno Setup drives installation. Runtime management never falls through
// to the retired message-box installer.
func runInnoInstallWorker(args []string) int {
	if err := validateInnoInvocation(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if !isAdmin() {
		fmt.Fprintln(os.Stderr, "Run the Inno Setup installer with administrator approval.")
		return 2
	}
	if err := installFromInno(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("OnTechChat installation completed.")
	return 0
}

func installFromInno() error {
	unlock, err := setupMutex()
	if err != nil {
		return err
	}
	defer unlock()
	p, err := installPaths()
	if err != nil {
		return err
	}
	if err = install(p); err != nil {
		return err
	}
	// Service installation succeeded; a desktop session might not exist in
	// unattended installs. Log this separately without undoing a working service.
	if err = launchTrayAsDesktopUser(p); err != nil {
		fmt.Fprintln(os.Stderr, "Tray starts at next sign-in:", err)
	}
	return nil
}

// Refuse an ambiguous invocation before paths, mutexes or services are touched.
func validateInnoInvocation(args []string) error {
	if len(args) != 1 || args[0] != "--install-worker" {
		return errors.New("unexpected installation worker arguments")
	}
	return nil
}
