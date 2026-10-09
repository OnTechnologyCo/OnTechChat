package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const passwordRounds = 600000
const passwordMinChars = 8
const passwordMaxBytes = 128
const passwordPolicyMessage = "비밀번호는 8자 이상이어야 합니다. 최대 UTF-8 128바이트이며 조합 조건은 없습니다."

func validNewPassword(p string) bool {
	return utf8.ValidString(p) && utf8.RuneCountInString(p) >= passwordMinChars && len(p) <= passwordMaxBytes
}

func randomID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func randomToken() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func digest(s string) string { x := sha256.Sum256([]byte(s)); return hex.EncodeToString(x[:]) }
func now() int64             { return time.Now().Unix() }

// PBKDF2-HMAC-SHA256, 32-byte output. Test vectors are cross-checked with Python hashlib.
func derivePassword(password, salt []byte, rounds int) []byte {
	mac := hmac.New(sha256.New, password)
	mac.Write(salt)
	var block [4]byte
	binary.BigEndian.PutUint32(block[:], 1)
	mac.Write(block[:])
	u := mac.Sum(nil)
	out := append([]byte(nil), u...)
	for i := 1; i < rounds; i++ {
		mac.Reset()
		mac.Write(u)
		u = mac.Sum(u[:0])
		for j := range out {
			out[j] ^= u[j]
		}
	}
	return out
}
func passwordHash(p string) (string, error) {
	if !validNewPassword(p) {
		return "", errors.New(passwordPolicyMessage)
	}
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		return "", e
	}
	key := derivePassword([]byte(p), salt, passwordRounds)
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordRounds, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}
func passwordMatches(p, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || len(p) > 128 {
		return false
	}
	rounds, e := strconv.Atoi(parts[1])
	if e != nil || rounds < 100000 || rounds > 2000000 {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(parts[2])
	if e != nil || len(salt) != 16 {
		return false
	}
	want, e := base64.RawStdEncoding.DecodeString(parts[3])
	if e != nil || len(want) != 32 {
		return false
	}
	return subtle.ConstantTimeCompare(derivePassword([]byte(p), salt, rounds), want) == 1
}
func newBox(key []byte) (cipher.AEAD, error) {
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
}
func encrypt(box cipher.AEAD, plain []byte) ([]byte, error) {
	nonce := make([]byte, box.NonceSize())
	if _, e := rand.Read(nonce); e != nil {
		return nil, e
	}
	return box.Seal(nonce, nonce, plain, []byte("OnTechChat-store-v1")), nil
}
func decrypt(box cipher.AEAD, b []byte) ([]byte, error) {
	n := box.NonceSize()
	if len(b) < n {
		return nil, errors.New("invalid encrypted store")
	}
	return box.Open(nil, b[:n], b[n:], []byte("OnTechChat-store-v1"))
}

type rateEntry struct {
	count int
	until time.Time
}
type Limiter struct {
	mu      sync.Mutex
	entries map[string]rateEntry
}

func (l *Limiter) allow(key string, max int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = map[string]rateEntry{}
	}
	t := time.Now()
	v := l.entries[key]
	if t.After(v.until) {
		v = rateEntry{until: t.Add(window)}
	}
	if v.count >= max {
		return false
	}
	if len(l.entries) > 10000 {
		for k, e := range l.entries {
			if t.After(e.until) {
				delete(l.entries, k)
			}
		}
		if len(l.entries) > 10000 {
			return false
		}
	}
	v.count++
	l.entries[key] = v
	return true
}
