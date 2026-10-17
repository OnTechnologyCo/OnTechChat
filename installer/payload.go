package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

//go:embed payload.zip
var payload []byte

const installerVersion = "1.15.34"
const serviceName = "OnTechChatServer"
const installID = "ontechdev.chatserver.windows.v1"

type installRecord struct {
	ID            string `json:"id"`
	Version       string `json:"version"`
	ProgramDir    string `json:"program_dir"`
	DataDir       string `json:"data_dir"`
	PayloadSHA256 string `json:"payload_sha256"`
}

func inspectPayload() ([]*zip.File, error) {
	z, e := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if e != nil {
		return nil, e
	}
	total := uint64(0)
	seen := map[string]bool{}
	apkManifest := false
	apkCount := 0
	for _, f := range z.File {
		name := f.Name
		if name == "" || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") || path.Clean(name) != name || name == ".." || strings.HasPrefix(name, "../") || f.FileInfo().IsDir() || f.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unsafe payload name: %q", name)
		}
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, ".exe") {
			return nil, errors.New("single-runtime package must not embed executable copies")
		}
		if strings.HasPrefix(lower, "apk-release/") {
			switch {
			case lower == "apk-release/latest.json":
				if f.UncompressedSize64 == 0 || f.UncompressedSize64 > 16<<10 {
					return nil, errors.New("invalid embedded APK manifest size")
				}
				apkManifest = true
			case strings.HasSuffix(lower, ".apk"):
				base := strings.TrimSuffix(strings.TrimPrefix(lower, "apk-release/"), ".apk")
				digest, de := hex.DecodeString(base)
				if len(base) != 64 || de != nil || len(digest) != 32 || f.UncompressedSize64 < 1024 || f.UncompressedSize64 > 150<<20 {
					return nil, errors.New("invalid embedded APK payload")
				}
				apkCount++
			default:
				return nil, errors.New("unexpected embedded APK bundle file")
			}
		}
		if seen[lower] {
			return nil, errors.New("duplicate payload filename")
		}
		seen[lower] = true
		total += f.UncompressedSize64
		if total > 80<<20 {
			return nil, errors.New("payload exceeds installer limit")
		}
	}
	if !seen["config.example.json"] || !seen["docs/api.md"] || !apkManifest || apkCount != 1 {
		return nil, errors.New("payload is incomplete")
	}
	return z.File, nil
}
func payloadDigest() string { h := sha256.Sum256(payload); return hex.EncodeToString(h[:]) }
func extractPayload(destination string) error {
	files, e := inspectPayload()
	if e != nil {
		return e
	}
	for _, f := range files {
		out := filepath.Join(destination, filepath.FromSlash(f.Name))
		if e = os.MkdirAll(filepath.Dir(out), 0755); e != nil {
			return e
		}
		r, e := f.Open()
		if e != nil {
			return e
		}
		w, e := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if e != nil {
			r.Close()
			return e
		}
		_, copyErr := io.Copy(w, io.LimitReader(r, int64(f.UncompressedSize64)+1))
		closeErr := w.Close()
		r.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
