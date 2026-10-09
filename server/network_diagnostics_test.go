package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNetworkDescriptionOnlyAndNoSecret(t *testing.T) {
	c := defaults()
	c.PublicURL = "https://203.0.113.18:49333"
	c.Mail.PasswordEncrypted = "NEVER_INCLUDE_SECRET"
	d := describeNetworkConfiguration(c)
	if !d.Loopback || d.ListenPort != 8787 || d.PublicPort != 49333 || d.TLSConfigured || len(d.Warnings) != 3 {
		t.Fatal(d)
	}
	b, _ := json.Marshal(networkSettingsInfo(c))
	if strings.Contains(string(b), "NEVER_INCLUDE_SECRET") {
		t.Fatal("secret leaked")
	}
	if !strings.Contains(string(b), `"tested_from_internet":false`) {
		t.Fatal("diagnostic must not claim external test")
	}
}
func TestNetworkDescriptionDirectTLS(t *testing.T) {
	c := defaults()
	c.Listen = "0.0.0.0:49333"
	c.PublicURL = "https://203.0.113.18:49333"
	c.TLSCert = "cert"
	c.TLSKey = "key"
	d := describeNetworkConfiguration(c)
	if d.Loopback || !d.TLSConfigured || len(d.Warnings) != 0 {
		t.Fatal(d)
	}
	c.Listen = "[::]:443"
	c.PublicURL = "https://chat.example.test"
	if d := describeNetworkConfiguration(c); d.PublicPort != 443 || d.ListenPort != 443 {
		t.Fatal(d)
	}
}
