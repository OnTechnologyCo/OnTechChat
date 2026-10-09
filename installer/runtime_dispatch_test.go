package main

import "testing"

func TestUnifiedRuntimeDispatch(t *testing.T) {
	for _, s := range []string{"--service", "--server-console", "--version", "--init", "--check-config", "--migrate-config"} {
		if !runtimeServerMode([]string{s}) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"--install", "--uninstall", "--tray", "--status", "--start", "--stop", "--restart", "--firewall", "--reset-admin"} {
		if runtimeServerMode([]string{s}) {
			t.Fatal(s)
		}
	}
	if !runtimeServerMode([]string{"--config", "example.json", "--reset-admin"}) {
		t.Fatal("worker missing")
	}
}
