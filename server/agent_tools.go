package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type AgentRunner interface {
	Run(context.Context, agentRunRequest) (agentRunResult, error)
}

func safeAgentRelativePath(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "/", "\\"))
	if raw == "" || filepath.IsAbs(raw) || filepath.VolumeName(raw) != "" ||
		strings.HasPrefix(raw, "\\") || strings.Contains(raw, ":") ||
		strings.ContainsRune(raw, 0) {
		return "", bad("프로젝트 내부 상대경로만 사용할 수 있습니다.")
	}
	clean := filepath.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "..\\") {
		return "", bad("프로젝트 경로를 벗어날 수 없습니다.")
	}
	return clean, nil
}
func resolveAgentPath(root, relative string, allowMissing bool) (string, error) {
	rel, err := safeAgentRelativePath(relative)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, rel)
	check := root
	parts := strings.Split(rel, string(filepath.Separator))
	for i, part := range parts {
		check = filepath.Join(check, part)
		info, statErr := os.Lstat(check)
		if statErr != nil {
			if allowMissing && errors.Is(statErr, os.ErrNotExist) {
				if i == len(parts)-1 {
					break
				}
				continue
			}
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", bad("링크 또는 reparse 경로는 사용할 수 없습니다.")
		}
	}
	finalAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	relCheck, err := filepath.Rel(root, finalAbs)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) || filepath.IsAbs(relCheck) {
		return "", forbidden()
	}
	return finalAbs, nil
}
func agentEnvironment(workspace, goroot string) (map[string]string, error) {
	if workspace == "" || !filepath.IsAbs(workspace) || (goroot != "" && !filepath.IsAbs(goroot)) {
		return nil, errors.New("agent workspace unavailable")
	}
	for _, name := range []string{
		"gocache", "gopath", "home", "home\\AppData\\Local",
		"home\\AppData\\Roaming", "tmp",
	} {
		if err := os.MkdirAll(filepath.Join(workspace, name), 0700); err != nil {
			return nil, err
		}
	}
	env := map[string]string{
		"SystemRoot": os.Getenv("SystemRoot"), "WINDIR": os.Getenv("WINDIR"),
		"SystemDrive": os.Getenv("SystemDrive"), "ComSpec": os.Getenv("ComSpec"),
		"PATHEXT": os.Getenv("PATHEXT"), "ProgramFiles": os.Getenv("ProgramFiles"),
		"PATH":    filepath.Join(os.Getenv("SystemRoot"), "System32"),
		"GOCACHE": filepath.Join(workspace, "gocache"),
		"GOPATH":  filepath.Join(workspace, "gopath"), "GOENV": "off",
		"GOFLAGS": "-p=1", "CGO_ENABLED": "0", "GOOS": "windows", "GOARCH": "amd64",
		"HOME": filepath.Join(workspace, "home"), "USERPROFILE": filepath.Join(workspace, "home"),
		"LOCALAPPDATA": filepath.Join(workspace, "home", "AppData", "Local"),
		"APPDATA":      filepath.Join(workspace, "home", "AppData", "Roaming"),
		"TEMP":         filepath.Join(workspace, "tmp"), "TMP": filepath.Join(workspace, "tmp"),
	}
	if goroot != "" {
		env["GOROOT"] = goroot
	}
	return env, nil
}
func listWorkspace(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return bad("프로젝트에 링크 경로가 있어 작업을 중단했습니다.")
		}
		if d.IsDir() {
			if len(out) >= 200 {
				return filepath.SkipDir
			}
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		if len(out) >= 200 {
			return errors.New("workspace file list limit")
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func readWorkspaceFile(root, rel string, max int) (string, error) {
	path, err := resolveAgentPath(root, rel, false)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(max) {
		return "", bad("읽을 수 없는 프로젝트 파일입니다.")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
func writeWorkspaceFile(root, rel, content string, max int) error {
	if len(content) > max {
		return bad("한 번에 작성할 수 있는 파일 크기를 초과했습니다.")
	}
	path, err := resolveAgentPath(root, rel, true)
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	parentRel, relErr := filepath.Rel(root, parent)
	if relErr != nil {
		return relErr
	}
	if parentRel != "." {
		if _, err = resolveAgentPath(root, parentRel, true); err != nil {
			return err
		}
	}
	if err = os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return bad("링크 파일은 덮어쓸 수 없습니다.")
	}
	tmp, err := os.CreateTemp(parent, ".agent-write-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.WriteString(content)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
func fileSHA256(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	if _, err = io.Copy(h, io.LimitReader(f, (1<<30)+1)); err != nil {
		return "", 0, err
	}
	if info.Size() > 1<<30 {
		return "", 0, errors.New("artifact exceeds hash limit")
	}
	return hex.EncodeToString(h.Sum(nil)), info.Size(), nil
}

func artifactMIME(name string) string {
	if v := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); v != "" {
		return v
	}
	return "application/octet-stream"
}
func workspaceSize(root string, limit int64) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return bad("프로젝트에 링크 경로가 있어 작업을 중단했습니다.")
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if limit > 0 && total > limit {
			return bad("프로젝트 저장 용량 한도를 초과했습니다.")
		}
		return nil
	})
	return total, err
}
