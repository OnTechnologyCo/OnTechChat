//go:build windows

package server

import (
	"encoding/binary"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

func decodeRegistryUTF16(raw []byte) string {
	if len(raw)%2 != 0 {
		return ""
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	return strings.TrimRight(string(utf16.Decode(units)), "\x00")
}

// 레지스트리 문자열을 UTF-16 API로 읽어 한글 사용자 경로의 코드페이지 손실을 피한다.
func readCLIRegistryString(root syscall.Handle, path, name string) string {
	keyName, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	var key syscall.Handle
	if syscall.RegOpenKeyEx(root, keyName, 0, syscall.KEY_QUERY_VALUE|syscall.KEY_WOW64_64KEY, &key) != nil {
		return ""
	}
	defer syscall.RegCloseKey(key)
	valueName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return ""
	}
	var typ, size uint32
	if syscall.RegQueryValueEx(key, valueName, nil, &typ, nil, &size) != nil || size == 0 || size > 131072 {
		return ""
	}
	if typ != syscall.REG_SZ && typ != syscall.REG_EXPAND_SZ {
		return ""
	}
	raw := make([]byte, size)
	if syscall.RegQueryValueEx(key, valueName, nil, &typ, &raw[0], &size) != nil {
		return ""
	}
	return decodeRegistryUTF16(raw[:size])
}

func listCLIRegistryKeys(root syscall.Handle, path string) []string {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil
	}
	var key syscall.Handle
	if syscall.RegOpenKeyEx(root, name, 0, syscall.KEY_ENUMERATE_SUB_KEYS|syscall.KEY_WOW64_64KEY, &key) != nil {
		return nil
	}
	defer syscall.RegCloseKey(key)
	var names []string
	for i := uint32(0); i < 256; i++ {
		var buf [512]uint16
		n := uint32(len(buf))
		err := syscall.RegEnumKeyEx(key, i, &buf[0], &n, nil, nil, nil, nil)
		if err != nil {
			break
		}
		names = append(names, syscall.UTF16ToString(buf[:n]))
	}
	return names
}

func nativeWindowsCLIProfiles() []windowsProfileEntry {
	const base = "SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion\\ProfileList"
	var entries []windowsProfileEntry
	for _, sid := range listCLIRegistryKeys(syscall.HKEY_LOCAL_MACHINE, base) {
		path := expandWindowsProfileValue(readCLIRegistryString(syscall.HKEY_LOCAL_MACHINE, base+"\\"+sid, "ProfileImagePath"))
		if path != "" && filepath.IsAbs(path) && !strings.HasPrefix(path, "\\\\") {
			entries = append(entries, windowsProfileEntry{SID: sid, Path: path})
		}
	}
	return entries
}

func nativeWindowsCLIUserEnv(name string) map[string]string {
	values := map[string]string{}
	// 로드된 사용자 Environment의 지정 값만 읽으며 HKU 전체를 재귀 검색하지 않는다.
	for _, sid := range listCLIRegistryKeys(syscall.HKEY_USERS, "") {
		if !strings.HasPrefix(sid, "S-1-") || strings.HasSuffix(sid, "_Classes") {
			continue
		}
		if value := readCLIRegistryString(syscall.HKEY_USERS, sid+"\\Environment", name); value != "" {
			values[sid] = value
		}
	}
	return values
}
