package server

import (
	"encoding/json"
	"os/exec"
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
	if !strings.Contains(csp, "style-src 'self'; style-src-attr 'unsafe-inline';") {
		t.Fatal("NAVER requires style attributes, without enabling inline scripts or style elements")
	}
	for _, required := range []string{"script-src 'self' https://oapi.map.naver.com/openapi/v3/maps.js https://oapi.map.naver.com/v3/auth ", "worker-src 'self';", "https://tiles.openfreemap.org/planet", "https://tiles.openfreemap.org/fonts/"} {
		if !strings.Contains(csp, required) {
			t.Fatalf("missing CSP: %s", required)
		}
	}
	if strings.Contains(csp, "*") || strings.Contains(csp, "unsafe-eval") || strings.Contains(strings.SplitN(csp, "style-src ", 2)[0], "unsafe-inline") || strings.Contains(csp, "https:;") {
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

func TestLocationNaverAssetsAndPolicy(t *testing.T) {
	_, s, _, _, _ := testApp(t)
	code, page, h := request(t, s, "", "GET", "/", nil)
	expectStatus(t, code, 200, page)
	if !strings.Contains(string(page), "/location-naver.js?v=1.15.33") {
		t.Fatal("NAVER renderer not wired into the history page")
	}
	if h.Get("Referrer-Policy") != "strict-origin-when-cross-origin" {
		t.Fatal("map authentication must receive only the cross-origin origin")
	}
	scriptPolicy := strings.Split(strings.SplitN(h.Get("Content-Security-Policy"), "script-src ", 2)[1], ";")[0]
	for _, path := range []string{
		"https://nrbe.pstatic.net/styles/basic@2x.json", "https://nrbe.pstatic.net/styles/terrain@2x.json", "https://nrbe.pstatic.net/styles/satellite@2x.json",
	} {
		if !strings.Contains(scriptPolicy, path) {
			t.Fatalf("missing live SDK JSONP metadata source: %s", path)
		}
	}
	for _, source := range strings.Fields(scriptPolicy) {
		if strings.HasSuffix(source, "/") {
			t.Fatalf("external script source must be an exact file: %s", source)
		}
	}
	for _, path := range []string{
		"https://map.pstatic.net/nrb/styles/", "https://nrbe.pstatic.net/styles/",
		"https://ssl.pstatic.net/static/maps/mantle/",
	} {
		if !strings.Contains(h.Get("Content-Security-Policy"), path) {
			t.Fatalf("missing NAVER resource scope: %s", path)
		}
	}
	code, js, _ := request(t, s, "", "GET", "/location-naver.js", nil)
	expectStatus(t, code, 200, js)
	if len(js) == 0 {
		t.Fatal("empty NAVER renderer")
	}
	// Adding the SDK must not expose arbitrary embedded web/test files.
	code, body, _ := request(t, s, "", "GET", "/location-naver-private.js", nil)
	expectStatus(t, code, 404, body)
}

func TestLocationNaverLifecycle(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is not installed; NAVER lifecycle mock checks were not run")
	}
	output, err := exec.Command(node, "testdata/location-naver.test.cjs").CombinedOutput()
	if err != nil {
		t.Fatalf("NAVER lifecycle checks: %v\n%s", err, output)
	}
	t.Log(string(output))
}
