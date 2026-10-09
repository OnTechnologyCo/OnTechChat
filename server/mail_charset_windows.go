package server

import (
	"errors"
	"io"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// Windows 설치판의 EUC-KR/CP949 헤더와 본문을 UTF-8로 변환합니다.
// https://learn.microsoft.com/windows/win32/api/stringapiset/nf-stringapiset-multibytetowidechar
func decodeAdditionalMailCharset(name string, r io.Reader) (io.Reader, error) {
	var page uintptr
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "euc-kr", "ks_c_5601-1987", "ks_c_5601-1989", "ksc5601", "cp949", "ms949", "windows-949":
		page = 949
	case "windows-1252", "cp1252":
		page = 1252
	default:
		return nil, errors.New("지원하지 않는 메일 문자 인코딩입니다.")
	}
	b, e := io.ReadAll(io.LimitReader(r, maxDecodedMail+1))
	if e != nil {
		return nil, errors.New("메일 문자 디코딩 중 읽기 실패")
	}
	if len(b) > maxDecodedMail {
		return nil, errMailTooLarge
	}
	if len(b) == 0 {
		return strings.NewReader(""), nil
	}
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("MultiByteToWideChar")
	n, _, _ := proc.Call(page, 8, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0, 0)
	if n == 0 || n > maxDecodedMail {
		return nil, errors.New("유효하지 않은 메일 문자 데이터입니다.")
	}
	out := make([]uint16, int(n))
	got, _, _ := proc.Call(page, 8, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&out[0])), n)
	runtime.KeepAlive(b)
	if got != n {
		return nil, errors.New("메일 문자 변환에 실패했습니다.")
	}
	return strings.NewReader(string(utf16.Decode(out))), nil
}
