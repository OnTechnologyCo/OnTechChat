package main

import (
	"errors"
	"fmt"
)

type removalSteps struct {
	firewall, protocol, startup, shortcuts func() error
	stop, service, data, program, registry func() error
}

func runRemoval(purgeData bool, s removalSteps) error {
	var failures []error
	for _, step := range []struct {
		name string
		run  func() error
	}{
		{"방화벽 규칙", s.firewall}, {"로컬 관리 연결", s.protocol},
		{"자동 시작 등록", s.startup}, {"바로가기", s.shortcuts},
	} {
		if e := step.run(); e != nil {
			failures = append(failures, fmt.Errorf("%s: %w", step.name, e))
		}
	}
	if e := s.stop(); e != nil {
		return errors.Join(append(failures, e)...)
	}
	if e := s.service(); e != nil {
		return errors.Join(append(failures, e)...)
	}
	// Preserve the executable and removal registration when cleanup needs a retry.
	if e := errors.Join(failures...); e != nil {
		return e
	}
	if purgeData {
		if e := s.data(); e != nil {
			return e
		}
	}
	if e := s.program(); e != nil {
		return e
	}
	return s.registry()
}
