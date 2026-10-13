package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLocationMapAssetsAndCSP(t *testing.T) {
	_, s, _, _, _ := testApp(t)
	assets := []string{"maplibre-gl-csp.js", "maplibre-gl-csp-worker.js", "maplibre-gl.css", "openfreemap-liberty-ko.json", "openfreemap-sprite.json", "openfreemap-sprite.png", "openfreemap-sprite@2x.json", "openfreemap-sprite@2x.png"}
	for _, asset := range assets {
		code, b, h := request(t, s, "", "GET", "/vendor/"+asset, nil)
		expectStatus(t, code, 200, b)
		if len(b) == 0 {
			t.Fatalf("empty map asset: %s", asset)
		}
		if strings.HasSuffix(asset, ".js") && !strings.Contains(h.Get("Content-Type"), "javascript") {
			t.Fatalf("worker/script MIME: %s", h.Get("Content-Type"))
		}
	}
	_, _, h := request(t, s, "", "GET", "/", nil)
	csp := h.Get("Content-Security-Policy")
	for _, required := range []string{"script-src 'self';", "worker-src 'self';", "https://tiles.openfreemap.org/planet", "https://tiles.openfreemap.org/fonts/"} {
		if !strings.Contains(csp, required) {
			t.Fatalf("missing CSP: %s", required)
		}
	}
	if strings.Contains(csp, "unsafe-eval") || strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "https:;") {
		t.Fatal("map integration weakened script policy")
	}
	code, b, _ := request(t, s, "", "GET", "/vendor/private.json", nil)
	expectStatus(t, code, 404, b)
	_, b, _ = request(t, s, "", "GET", "/vendor/openfreemap-liberty-ko.json", nil)
	var style struct {
		Sprite  string `json:"sprite"`
		Sources map[string]struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"sources"`
		Layers []struct {
			ID     string                     `json:"id"`
			Layout map[string]json.RawMessage `json:"layout"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(b, &style); err != nil {
		t.Fatal(err)
	}
	if style.Sprite != "/vendor/openfreemap-sprite" || len(style.Sources) != 1 || style.Sources["openmaptiles"].URL != "https://tiles.openfreemap.org/planet" {
		t.Fatal("unexpected external style dependency")
	}
	names := 0
	for _, layer := range style.Layers {
		field := string(layer.Layout["text-field"])
		if strings.Contains(field, "name") {
			names++
			if !strings.Contains(field, "name:ko") {
				t.Fatalf("missing Korean preference: %s", layer.ID)
			}
		}
	}
	if names == 0 {
		t.Fatal("no localized labels")
	}
}
