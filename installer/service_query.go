package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf16"
)

const maxServiceConfigBytes uint32 = 8192

var errServiceBufferTooSmall = errors.New("service configuration buffer is too small")

type serviceConfigQuery func(buffer []byte) (needed uint32, err error)

// QueryServiceConfigW 한도(8 KiB)를 넘기지 않습니다. 1.2.1 수정사항 유지.
func readServiceConfigBuffer(query serviceConfigQuery) ([]byte, error) {
	needed, err := query(nil)
	if !errors.Is(err, errServiceBufferTooSmall) {
		if err == nil {
			return nil, errors.New("service size query returned unexpected success")
		}
		return nil, fmt.Errorf("service configuration size query: %w", err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		if needed == 0 || needed > maxServiceConfigBytes {
			return nil, fmt.Errorf("invalid service configuration size: %d (maximum %d)", needed, maxServiceConfigBytes)
		}
		buf := make([]byte, int(needed))
		next, err := query(buf)
		if err == nil {
			return buf, nil
		}
		if !errors.Is(err, errServiceBufferTooSmall) {
			return nil, fmt.Errorf("service configuration read: %w", err)
		}
		if next <= uint32(len(buf)) {
			return nil, fmt.Errorf("service configuration size did not increase: %d", next)
		}
		needed = next
	}
	return nil, errors.New("service configuration changed repeatedly; close other installers and retry")
}
func decodeServiceUTF16(buf []byte, offset int) (string, error) {
	if offset < 0 || offset >= len(buf) || offset%2 != 0 {
		return "", errors.New("service string pointer is outside returned buffer")
	}
	values := make([]uint16, 0, (len(buf)-offset)/2)
	for i := offset; i+1 < len(buf); i += 2 {
		value := binary.LittleEndian.Uint16(buf[i : i+2])
		if value == 0 {
			return string(utf16.Decode(values)), nil
		}
		values = append(values, value)
	}
	return "", errors.New("service string is not terminated within returned buffer")
}
