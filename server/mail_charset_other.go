//go:build !windows

package server

import (
	"errors"
	"io"
)

func decodeAdditionalMailCharset(name string, r io.Reader) (io.Reader, error) {
	return nil, errors.New("이 실행 환경에서 지원하지 않는 메일 문자 인코딩입니다.")
}
