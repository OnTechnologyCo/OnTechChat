package main

import (
	"strings"
	"testing"
)

func TestServiceRegistrationPreservesExistingAccount(t *testing.T) {
	binary := `"D:\앱 경로\OnTechChat.Server.exe" --service`
	for _, existed := range []bool{false, true} {
		args := serviceRegistrationArgs(existed, binary)
		if args[3] != binary {
			t.Fatal("binary path changed")
		}
		want := "create"
		if existed {
			want = "config"
		}
		if args[0] != want {
			t.Fatal("wrong registration action")
		}
		joined := strings.Join(args, "|")
		if strings.Contains(joined, "password") {
			t.Fatal("credentials must not be passed")
		}
		if existed && strings.Contains(joined, "obj=") {
			t.Fatal("update overwrites service account")
		}
		if !existed && !strings.Contains(joined, `obj=|NT AUTHORITY\LocalService`) {
			t.Fatal("fresh install default lost")
		}
	}
}
