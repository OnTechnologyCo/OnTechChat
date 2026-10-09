package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func mediaPNG(t *testing.T) []byte {
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	if e := png.Encode(&b, img); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestAIAttachmentOwnershipVisionAndDeletedSource(t *testing.T) {
	a, s, _, bob, eve := testApp(t)
	a.cfg.AI = aiTestConfig()
	room := randomID()
	file := randomID()
	data := mediaPNG(t)
	hash := sha256.Sum256(data)
	os.MkdirAll(filepath.Join(a.cfg.DataDir, "files"), 0700)
	os.WriteFile(filepath.Join(a.cfg.DataDir, "files", file), data, 0600)
	var source int64
	a.store.update(func(st *State) error {
		st.Rooms[room] = Room{ID: room, OwnerID: bob.ID, Kind: "ai", Members: map[string]int64{bob.ID: 0}}
		st.Files[file] = Attachment{ID: file, RoomID: room, UserID: bob.ID, Name: "photo.png", MIME: "image/png", Size: int64(len(data)), SHA256: hex.EncodeToString(hash[:])}
		m, _ := addMessage(st, Message{RoomID: room, UserID: bob.ID, Kind: "file", FileID: file, Text: "photo.png"}, a.cfg.MaxMessages, false)
		source = m.ID
		return nil
	})
	if _, e := a.prepareAIMedia(eve.ID, room, source+1); e == nil {
		t.Fatal("cross-user file read")
	}
	media, e := a.prepareAIMedia(bob.ID, room, source+1)
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(media.Dir)
	if len(media.Inputs) != 1 || media.Inputs[0].ImagePath == "" {
		t.Fatal("image not staged")
	}
	var parts int
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		var blocks []map[string]any
		json.Unmarshal(body.Messages[0].Content, &blocks)
		parts = len(blocks)
		if parts != 2 || !strings.HasPrefix(blocks[1]["image_url"].(map[string]any)["url"].(string), "data:image/png;base64,") {
			t.Error("missing visual payload")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"image seen"}}]}`))
	}))
	defer httpServer.Close()
	executor := newDefaultAIExecutor()
	_, e = executor.Execute(withAIMedia(context.Background(), media), AIProviderConfig{Type: "openai_compatible", BaseURL: httpServer.URL}, AIModelConfig{Model: "vision"}, "", []AIWireMessage{{Role: "user", Content: "describe"}})
	if e != nil || parts != 2 {
		t.Fatalf("vision request %v", e)
	}
	bt := loginTest(t, s, bob.Username)
	code, b, _ := request(t, s, bt, "DELETE", "/api/messages/"+fmtSprint(source), nil)
	expectStatus(t, code, 200, b)
	a.store.view(func(st *State) error {
		if a.validateAIMedia(st, bob.ID, room, media) == nil {
			t.Error("deleted source accepted")
		}
		return nil
	})
}
func fmtSprint(v int64) string { return strconv.FormatInt(v, 10) }
func TestAICodexGeneratedImagesThreadBoundary(t *testing.T) {
	root := t.TempDir()
	thread := "12345678-1234-1234-1234-123456789abc"
	dir := filepath.Join(root, "generated_images", thread)
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, "image.png"), mediaPNG(t), 0600)
	sibling := filepath.Join(root, "generated_images", "other")
	os.MkdirAll(sibling, 0700)
	os.WriteFile(filepath.Join(sibling, "keep.txt"), []byte("untouched"), 0600)
	media := &aiMedia{}
	e := collectCodexImages([]byte(`{"type":"thread.started","thread_id":"`+thread+`"}`), root, media)
	if e != nil || len(media.Generated) != 1 {
		t.Fatalf("image missing %v", e)
	}
	if _, e = os.Stat(dir); !os.IsNotExist(e) {
		t.Error("temporary generated copy retained")
	}
	if _, e = os.Stat(filepath.Join(sibling, "keep.txt")); e != nil {
		t.Error("other thread touched")
	}
}
func TestAICodexImageEditLive(t *testing.T) {
	if os.Getenv("ONTECH_TEST_CODEX_IMAGE_LIVE") != "1" {
		t.Skip("opt-in image editing live test")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.png")
	os.WriteFile(input, mediaPNG(t), 0600)
	media := &aiMedia{Inputs: []aiInputFile{{ImagePath: input}}}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	text, e := runCodex(withAIMedia(ctx, media), execAICommandRunner{}, AIProviderConfig{Type: "codex_cli", Executable: "codex"}, "gpt-6-astra", "low", "Use image_gen exactly once to edit the attached image: replace the red pixel with a solid blue circle on white. Use num_last_images_to_include=1. No other tools. Return DONE after the image tool succeeds.", "")
	if e != nil || len(media.Generated) == 0 {
		t.Fatalf("image edit not delivered: %v; final=%q", e, text)
	}
	t.Logf("actual image editing returned %d verified image(s)", len(media.Generated))
}

func TestAIAttachmentUploadClearAndAssets(t *testing.T) {
	a, s, _, bob, eve := testApp(t)
	a.cfg.AI = aiTestConfig()
	bt := loginTest(t, s, bob.Username)
	et := loginTest(t, s, eve.Username)
	code, b, _ := request(t, s, bt, "POST", "/api/rooms/ai", map[string]any{"model_id": "test-model"})
	expectStatus(t, code, 201, b)
	room := decodeMap(t, b)["id"].(string)
	code, b = uploadTest(t, s, et, room, randomID(), "private.png", mediaPNG(t))
	expectStatus(t, code, 403, b)
	code, b = uploadTest(t, s, bt, room, randomID(), "photo.png", mediaPNG(t))
	expectStatus(t, code, 201, b)
	id := decodeMap(t, b)["file"].(map[string]any)["id"].(string)
	code, b, _ = request(t, s, bt, "DELETE", "/api/rooms/"+room+"/ai/messages", nil)
	expectStatus(t, code, 200, b)
	code, b, _ = request(t, s, bt, "GET", "/api/files/"+id, nil)
	expectStatus(t, code, 404, b)
	if _, e := os.Stat(filepath.Join(a.cfg.DataDir, "files", id)); !os.IsNotExist(e) {
		t.Fatal("cleared image remains on disk")
	}
	for _, path := range []string{"/navigation.js", "/composer.js", "/composer.css", "/conversation-auto.js", "/location-map.js", "/vendor/leaflet.js", "/vendor/leaflet.css"} {
		res, e := s.Client().Get(s.URL + path)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("asset missing %s: %d", path, res.StatusCode)
		}
	}
}
