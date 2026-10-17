package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAutoHTTPSFirewallPlanIsSeparateAndFixed(t *testing.T) {
	p, e := makeHTTPSFirewallPlan([]byte(`{"auto_https":{"enabled":true,"domain":"chat.example.com"},"public_url":"https://203.0.113.1:49333","mail":{"password":"SECRET"}}`), "server.exe")
	if e != nil || p.Port != 443 || p.RuleName == firewallRuleName || p.RuleGroup == firewallRuleGroup || p.PublicURL != "https://chat.example.com" {
		t.Fatal(p, e)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "SECRET") {
		t.Fatal("secret leak")
	}
	for _, raw := range []string{`{}`, `{"auto_https":{"enabled":false,"domain":"chat.example.com"}}`, `{"auto_https":{"enabled":true,"domain":"x.com;calc"}}`, `{"auto_https":{"enabled":true,"domain":"175.119.187.91"}}`} {
		if _, e := makeHTTPSFirewallPlan([]byte(raw), "server.exe"); e == nil {
			t.Fatal(raw)
		}
	}
}
func TestAutoHTTPSFirewallProtocolRejectsArguments(t *testing.T) {
	for _, v := range []string{httpsFirewallProtocol, httpsFirewallProtocol + "/"} {
		if !validHTTPSFirewallProtocol(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{httpsFirewallProtocol + "?port=22", httpsFirewallProtocol + ";calc", httpsFirewallProtocol + "#a", httpsFirewallProtocol + " --uninstall"} {
		if validHTTPSFirewallProtocol(v) {
			t.Fatal(v)
		}
	}
}
