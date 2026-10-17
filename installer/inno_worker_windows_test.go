//go:build windows

package main

import "testing"

func TestInnoWorkerRejectsMixedCommandsBeforeInstallation(t *testing.T) {
	for _, args := range [][]string{nil, {"--install"}, {"--install-worker", "--uninstall"}, {"--install-worker", "--config", "other.json"}, {"--protocol", "ontechchat-admin://https-firewall"}} {
		if validateInnoInvocation(args) == nil {
			t.Fatalf("accepted ambiguous worker command: %q", args)
		}
	}
	if err := validateInnoInvocation([]string{"--install-worker"}); err != nil {
		t.Fatal(err)
	}
}
