package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

func cleanFilename(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || !utf8.ValidString(name) || len(name) > 240 {
		return "", bad("파일 이름이 올바르지 않습니다.")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", bad("파일 이름에 제어 문자를 사용할 수 없습니다.")
		}
	}
	return name, nil
}
func (a *App) upload(w http.ResponseWriter, r *http.Request, u User) {
	roomID := r.PathValue("room")
	cid := r.Header.Get("X-Client-ID")
	if !clientIDPattern.MatchString(cid) {
		fail(w, bad("고유 X-Client-ID 헤더가 필요합니다."))
		return
	}
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	var duplicate *Message
	var used int64
	e := a.store.view(func(st *State) error {
		room, ok := member(st, roomID, u.ID)
		if !ok || !canUploadRoom(st, room, u.ID) {
			return forbidden()
		}
		for _, m := range st.Messages {
			if m.RoomID == roomID && m.UserID == u.ID && m.ClientID == cid {
				copy := m
				duplicate = &copy
				break
			}
		}
		for _, f := range st.Files {
			used += f.Size
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	if duplicate != nil {
		respond(w, 200, duplicate)
		return
	}
	if r.ContentLength > a.cfg.MaxFileBytes+(1<<20) {
		fail(w, apiError{413, "파일 크기 제한을 초과했습니다."})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, a.cfg.MaxFileBytes+(1<<20))
	mr, e := r.MultipartReader()
	if e != nil {
		fail(w, bad("multipart/form-data 요청이 필요합니다."))
		return
	}
	part, e := mr.NextPart()
	if e != nil || part.FormName() != "file" {
		fail(w, bad("file 필드에 파일 한 개를 첨부해 주세요."))
		return
	}
	defer part.Close()
	name, e := cleanFilename(part.FileName())
	if e != nil {
		fail(w, e)
		return
	}
	dir := filepath.Join(a.cfg.DataDir, "files")
	tmp, e := os.CreateTemp(dir, ".upload-*")
	if e != nil {
		fail(w, e)
		return
	}
	tempName := tmp.Name()
	defer os.Remove(tempName)
	if e = tmp.Chmod(0600); e != nil {
		tmp.Close()
		fail(w, e)
		return
	}
	hash := sha256.New()
	prefix := make([]byte, 512)
	n, e := io.ReadFull(part, prefix)
	if e != nil && e != io.EOF && e != io.ErrUnexpectedEOF {
		tmp.Close()
		fail(w, bad("파일을 읽지 못했습니다."))
		return
	}
	prefix = prefix[:n]
	writer := io.MultiWriter(tmp, hash)
	written, e := writer.Write(prefix)
	var rest int64
	if e == nil {
		rest, e = io.Copy(writer, io.LimitReader(part, a.cfg.MaxFileBytes-int64(written)+1))
	}
	size := int64(written) + rest
	if e == nil {
		e = tmp.Sync()
	}
	ce := tmp.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		var maxErr *http.MaxBytesError
		if errors.As(e, &maxErr) {
			fail(w, apiError{413, "파일 크기 제한을 초과했습니다."})
		} else {
			fail(w, bad("업로드가 중단되었습니다."))
		}
		return
	}
	if size > a.cfg.MaxFileBytes {
		fail(w, apiError{413, "파일 크기 제한을 초과했습니다."})
		return
	}
	if used+size > a.cfg.StorageQuotaBytes {
		fail(w, apiError{507, "서버 파일 저장 용량을 초과했습니다."})
		return
	}
	if _, e = mr.NextPart(); e != io.EOF {
		fail(w, bad("요청에는 파일 한 개만 포함할 수 있습니다."))
		return
	}
	file := Attachment{ID: randomID(), RoomID: roomID, UserID: u.ID, Name: name, MIME: http.DetectContentType(prefix), Size: size, SHA256: hex.EncodeToString(hash.Sum(nil)), Created: now()}
	dest := filepath.Join(dir, file.ID)
	if e = os.Rename(tempName, dest); e != nil {
		fail(w, e)
		return
	}
	syncDirectory(dir)
	var message Message
	e = a.store.update(func(st *State) error {
		room, ok := member(st, roomID, u.ID)
		if !ok || !canUploadRoom(st, room, u.ID) {
			return forbidden()
		}
		st.Files[file.ID] = file
		var e error
		message, e = addMessage(st, Message{RoomID: roomID, UserID: u.ID, Username: u.Username, Kind: "file", Text: name, FileID: file.ID, ClientID: cid}, a.cfg.MaxMessages, a.cfg.FCM.Enabled)
		return e
	})
	if e != nil {
		_ = os.Remove(dest)
		fail(w, e)
		return
	}
	a.changed(roomID)
	respond(w, 201, map[string]any{"message": message, "file": file})
}
func safeInline(m string) bool {
	switch strings.Split(m, ";")[0] {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "application/pdf", "text/plain":
		return true
	}
	return false
}
func (a *App) download(w http.ResponseWriter, r *http.Request, u User) {
	id := r.PathValue("id")
	if !fileIDPattern.MatchString(id) {
		fail(w, notfound())
		return
	}
	var f Attachment
	e := a.store.view(func(st *State) error {
		var ok bool
		f, ok = st.Files[id]
		if !ok {
			return notfound()
		}
		room, allowed := member(st, f.RoomID, u.ID)
		if !allowed {
			return forbidden()
		}
		if cutoff := roomClearedThrough(room, u.ID); cutoff > 0 {
			visible := false
			for _, m := range st.Messages {
				if m.RoomID == f.RoomID && m.FileID == id && m.ID > cutoff {
					visible = true
					break
				}
			}
			if !visible {
				return forbidden()
			}
		}
		return nil
	})
	if e != nil {
		fail(w, e)
		return
	}
	path := filepath.Join(a.cfg.DataDir, "files", id)
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() {
		fail(w, notfound())
		return
	}
	fd, e := os.Open(path)
	if e != nil {
		fail(w, notfound())
		return
	}
	defer fd.Close()
	disposition := "attachment"
	m := "application/octet-stream"
	if r.URL.Query().Get("inline") == "1" && safeInline(f.MIME) {
		disposition = "inline"
		m = f.MIME
	}
	w.Header().Set("Content-Type", m)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": f.Name}))
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox; frame-ancestors 'none'")
	http.ServeContent(w, r, f.Name, info.ModTime(), fd)
}
