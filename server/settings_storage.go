package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// 설정 비밀은 대화 저장소와 다른 AAD를 사용합니다. key 파일은 private 폴더 ACL로 보호됩니다.
func sealSetting(s *Store, purpose string, plain []byte) (string, error) {
	n := make([]byte, s.box.NonceSize())
	if _, e := rand.Read(n); e != nil {
		return "", e
	}
	return base64.RawStdEncoding.EncodeToString(s.box.Seal(n, n, plain, []byte("OnTechChat-config-v2/"+purpose))), nil
}
func openSetting(s *Store, purpose, value string) ([]byte, error) {
	b, e := base64.RawStdEncoding.DecodeString(value)
	n := s.box.NonceSize()
	if e != nil || len(b) < n {
		return nil, errors.New("invalid encrypted setting")
	}
	out, e := s.box.Open(nil, b[:n], b[n:], []byte("OnTechChat-config-v2/"+purpose))
	if e != nil {
		return nil, errors.New("cannot decrypt setting with this server key")
	}
	return out, nil
}
func unlockConfiguration(c *Config, s *Store) error {
	if c.RemoteControl.PasswordEncrypted != "" {
		b, e := openSetting(s, "remotecontrol-password", c.RemoteControl.PasswordEncrypted)
		if e != nil {
			return e
		}
		c.RemoteControl.password = string(b)
	}
	if c.Mail.PasswordEncrypted != "" {
		b, e := openSetting(s, "mail-password", c.Mail.PasswordEncrypted)
		if e != nil {
			return e
		}
		c.Mail.password = string(b)
	}
	if c.FCM.ServiceAccountEncrypted != "" {
		b, e := openSetting(s, "fcm-service-account", c.FCM.ServiceAccountEncrypted)
		if e != nil {
			return e
		}
		c.FCM.credentialsJSON = b
	}
	for i := range c.AI.Providers {
		p := &c.AI.Providers[i]
		if p.APIKeyEncrypted == "" {
			continue
		}
		b, e := openSetting(s, "ai-provider-key/"+p.ID, p.APIKeyEncrypted)
		if e != nil {
			return e
		}
		p.apiKey = string(b)
	}
	return nil
}
func boundedFile(path string, limit int64) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > limit {
		return nil, errors.New("file too large")
	}
	return b, nil
}
func configBytes(c Config) ([]byte, error) {
	// 런타임에서 절대경로로 해석한 데이터 경로를 저장할 때 설치 위치 기준으로 되돌립니다.
	if c.configPath != "" {
		base := filepath.Dir(c.configPath)
		for _, p := range []*string{&c.DataDir, &c.TLSCert, &c.TLSKey} {
			if *p != "" {
				rel, e := filepath.Rel(base, *p)
				if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					*p = rel
				}
			}
		}
	}
	b, e := json.MarshalIndent(c, "", "  ")
	return append(b, '\n'), e
}
func validateConfigTarget(path string) error {
	if path == "" {
		return errors.New("configuration path unavailable")
	}
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e == nil {
			if st.Mode()&os.ModeSymlink != 0 {
				return errors.New("symlink configuration paths are not supported")
			}
			if e = rejectConfigReparse(p); e != nil {
				return e
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}
func atomicConfigFile(path string, b []byte) error {
	if e := validateConfigTarget(path); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e != nil {
		f.Close()
		return e
	}
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = replaceConfigFile(name, path); e != nil {
		return e
	}
	syncDirectory(filepath.Dir(path))
	return nil
}
func persistConfiguration(c Config, expected string) (string, error) {
	if e := validateConfigTarget(c.configPath); e != nil {
		return "", e
	}
	old, e := boundedFile(c.configPath, 256<<10)
	if e != nil {
		return "", e
	}
	if digest(string(old)) != expected {
		return "", apiError{409, "다른 화면이나 프로그램에서 설정을 변경했습니다. 다시 읽은 후 저장하세요."}
	}
	b, e := configBytes(c)
	if e != nil {
		return "", e
	}
	// 먼저 이전 JSON을 보존하고, 새 JSON의 전체 쓰기·동기화가 끝난 후 교체합니다.
	if e = atomicConfigFile(c.configPath+".bak", old); e != nil {
		return "", e
	}
	if e = atomicConfigFile(c.configPath, b); e != nil {
		return "", e
	}
	return digest(string(b)), nil
}

type legacyMailValues struct{ Username, Password, Host, Folder string }

func parseLegacyINI(text string) (legacyMailValues, error) {
	var out legacyMailValues
	if len(text) > 64<<10 || !utf8.ValidString(text) || strings.ContainsRune(text, '\x00') {
		return out, bad("INI는 UTF-8 텍스트이며 64KiB 이하여야 합니다.")
	}
	section := ""
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimPrefix(text, "\ufeff"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		if section != "common" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		if k != "mailid" && k != "password" && k != "imapserver" && k != "mailfolder" {
			continue
		}
		if seen[k] {
			return out, bad("INI의 동일한 설정 항목이 중복되어 있습니다.")
		}
		seen[k] = true
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		switch k {
		case "mailid":
			out.Username = v
		case "password":
			out.Password = v
		case "imapserver":
			out.Host = v
		case "mailfolder":
			out.Folder = v
		}
	}
	if out.Username == "" || out.Password == "" {
		return out, bad("INI의 [COMMON] MailID와 Password가 필요합니다.")
	}
	if len(out.Password) > 1024 || strings.ContainsAny(out.Password, "\r\n\x00") {
		return out, bad("메일 비밀번호 형식을 확인하세요.")
	}
	return out, nil
}

// 구 버전 파일 참조는 시작 시 한 번만 JSON으로 이전합니다. 이후 수집 루프는 외부 INI/환경 변수를 읽지 않습니다.
func migrateConfiguration(c Config, s *Store) (Config, error) {
	legacy := c.Mail.LegacyINI != "" || c.Mail.UsernameEnv != "" || c.Mail.PasswordEnv != "" || c.FCM.CredentialsFile != ""
	if !legacy {
		return c, nil
	}
	notices := []string{"기존 설정을 JSON으로 통합했습니다. 원본 INI/키 파일은 자동 삭제하지 않았습니다."}
	if c.Mail.PasswordEncrypted == "" {
		u, p := os.Getenv(c.Mail.UsernameEnv), os.Getenv(c.Mail.PasswordEnv)
		if c.Mail.LegacyINI != "" {
			b, e := boundedFile(c.Mail.LegacyINI, 64<<10)
			var v legacyMailValues
			if e == nil {
				v, e = parseLegacyINI(string(b))
			}
			if e == nil {
				u, p = v.Username, v.Password
				if v.Host != "" {
					c.Mail.Host = v.Host
				}
				if v.Folder != "" {
					c.Mail.Folder = v.Folder
				}
			} else {
				notices = append(notices, "기존 INI를 읽지 못했습니다. 설정 화면에서 계정을 입력하거나 INI를 가져오세요.")
			}
		}
		if c.Mail.Username == "" {
			c.Mail.Username = u
		}
		if p != "" {
			var e error
			c.Mail.PasswordEncrypted, e = sealSetting(s, "mail-password", []byte(p))
			if e != nil {
				return c, e
			}
		}
	}
	if c.Mail.Enabled && (c.Mail.Username == "" || c.Mail.PasswordEncrypted == "") {
		c.Mail.Enabled = false
		notices = append(notices, "메일 자격증명 미설정으로 수집을 일시 해제했습니다.")
	}
	if c.FCM.CredentialsFile != "" && c.FCM.ServiceAccountEncrypted == "" {
		b, e := boundedFile(c.FCM.CredentialsFile, 64<<10)
		if e == nil {
			tmp := c.FCM
			tmp.credentialsJSON = b
			_, e = newFCM(tmp)
		}
		if e == nil {
			c.FCM.ServiceAccountEncrypted, e = sealSetting(s, "fcm-service-account", b)
			if e != nil {
				return c, e
			}
		} else {
			c.FCM.Enabled = false
			notices = append(notices, "Firebase 키를 이전하지 못했습니다. 설정 화면에서 서비스 계정 JSON을 가져오세요.")
		}
	}
	c.Mail.LegacyINI = ""
	c.Mail.UsernameEnv = ""
	c.Mail.PasswordEnv = ""
	c.FCM.CredentialsFile = ""
	c.SchemaVersion = 2
	if e := c.validate(); e != nil {
		c.Mail.Enabled = false
		c.FCM.Enabled = false
		notices = append(notices, "수집/알림 규칙을 확인한 후 다시 활성화하세요.")
		if e = c.validate(); e != nil {
			return c, e
		}
	}
	c.MigrationNotice = strings.Join(notices, " ")
	if c.configPath != "" {
		raw, e := boundedFile(c.configPath, 256<<10)
		if e != nil {
			return c, e
		}
		if _, e = persistConfiguration(c, digest(string(raw))); e != nil {
			return c, e
		}
	}
	return c, nil
}
