package main

import (
	"errors"
	"testing"
)

func TestServiceConfigTwoPass(t *testing.T) {
	n := 0
	b, e := readServiceConfigBuffer(func(b []byte) (uint32, error) {
		n++
		if len(b) == 0 {
			return 256, errServiceBufferTooSmall
		}
		if len(b) != 256 {
			t.Fatal(len(b))
		}
		b[0] = 65
		return 256, nil
	})
	if e != nil || n != 2 || b[0] != 65 {
		t.Fatal(n, e)
	}
}
func TestServiceConfigGrowsSafely(t *testing.T) {
	n := 0
	_, e := readServiceConfigBuffer(func(b []byte) (uint32, error) {
		n++
		if n == 1 {
			return 128, errServiceBufferTooSmall
		}
		if n == 2 {
			return 512, errServiceBufferTooSmall
		}
		if len(b) != 512 {
			t.Fatal(len(b))
		}
		return 512, nil
	})
	if e != nil || n != 3 {
		t.Fatal(n, e)
	}
}
func TestServiceConfigLimits(t *testing.T) {
	for _, size := range []uint32{0, 8193, 65536} {
		n := 0
		_, e := readServiceConfigBuffer(func(b []byte) (uint32, error) { n++; return size, errServiceBufferTooSmall })
		if e == nil || n != 1 {
			t.Fatal(size, n, e)
		}
	}
}
func TestServiceConfigErrors(t *testing.T) {
	for _, fn := range []serviceConfigQuery{func(b []byte) (uint32, error) { return 0, nil }, func(b []byte) (uint32, error) { return 0, errors.New("denied") }, func(b []byte) (uint32, error) { return 256, errServiceBufferTooSmall }} {
		if _, e := readServiceConfigBuffer(fn); e == nil {
			t.Fatal("expected error")
		}
	}
}
func TestServiceUTF16Bounded(t *testing.T) {
	u, e := decodeServiceUTF16([]byte{65, 0, 66, 0, 0, 0}, 0)
	if e != nil || u != "AB" {
		t.Fatal(u, e)
	}
	for _, off := range []int{-1, 1, 8} {
		if _, e := decodeServiceUTF16([]byte{0, 0}, off); e == nil {
			t.Fatal(off)
		}
	}
	if _, e := decodeServiceUTF16([]byte{65, 0}, 0); e == nil {
		t.Fatal("unterminated")
	}
}
func TestTrayEndpoint(t *testing.T) {
	for _, c := range []struct{ listen, tls, want string }{{"127.0.0.1:8787", "", "http://127.0.0.1:8787/"}, {"[::1]:9443", "tls.crt", "https://[::1]:9443/"}} {
		b, u, e := trayEndpoint(c.listen, c.tls)
		if e != nil || u != c.want || len(b) == 0 {
			t.Fatal(string(b), u, e)
		}
	}
	for _, s := range []string{"0.0.0.0:8787", ":8787", "example.com:443", "127.0.0.1:99999", "127.0.0.1:0"} {
		if _, _, e := trayEndpoint(s, ""); e == nil {
			t.Fatal("unsafe", s)
		}
	}
}
