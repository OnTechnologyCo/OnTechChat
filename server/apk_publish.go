package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Offline publisher. Only a verified, fixed-name APK and manifest are copied. Existing user data is not opened.
func publishAPK(c Config, source string) error {
	src, e := filepath.Abs(source)
	if e != nil {
		return e
	}
	rel, e := readAPKRelease(src)
	if e != nil {
		return fmt.Errorf("release bundle: %w", e)
	}
	f, e := openVerifiedAPK(src, rel)
	if e != nil {
		return e
	}
	defer f.Close()
	dir := updateFolder(c)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	st, e := os.Lstat(dir)
	if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid APK destination")
	}
	lock, e := os.OpenFile(filepath.Join(dir, ".publish.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("APK publisher already running; inspect .publish.lock before retry")
	}
	lock.Close()
	defer os.Remove(filepath.Join(dir, ".publish.lock"))
	if old, e := readAPKRelease(dir); e == nil {
		if rel.VersionCode < old.VersionCode {
			return errors.New("version downgrade rejected")
		}
		if rel.VersionCode == old.VersionCode && rel.SHA256 != old.SHA256 {
			return errors.New("same versionCode with different APK rejected")
		}
	} else if !os.IsNotExist(e) {
		return fmt.Errorf("existing release needs repair: %w", e)
	}
	target := filepath.Join(dir, rel.Filename)
	if old, e := openVerifiedAPK(dir, rel); e == nil {
		old.Close()
	} else if _, e = os.Lstat(target); !os.IsNotExist(e) {
		return errors.New("existing APK with this digest filename is invalid; inspect without overwriting")
	} else {
		out, e := os.CreateTemp(dir, ".apk-")
		if e != nil {
			return e
		}
		tmp := out.Name()
		defer os.Remove(tmp)
		if e = out.Chmod(0600); e != nil {
			out.Close()
			return e
		}
		_, e = io.Copy(out, f)
		if e == nil {
			e = out.Sync()
		}
		ce := out.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		if e = os.Rename(tmp, target); e != nil {
			return e
		}
	}
	b, e := json.MarshalIndent(rel, "", "  ")
	if e != nil {
		return e
	}
	out, e := os.CreateTemp(dir, ".manifest-")
	if e != nil {
		return e
	}
	tmp := out.Name()
	defer os.Remove(tmp)
	if e = out.Chmod(0600); e != nil {
		out.Close()
		return e
	}
	_, e = out.Write(append(b, '\n'))
	if e == nil {
		e = out.Sync()
	}
	ce := out.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	// Replace only a completed same-directory manifest. Retry sharing conflicts; never remove the old manifest first.
	for i := 0; i < 10; i++ {
		e = os.Rename(tmp, filepath.Join(dir, "latest.json"))
		if e == nil {
			fmt.Printf("Published Android %s (code %d). Server restart not required.\n", rel.VersionName, rel.VersionCode)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return e
}
