package server

import (
	"math"
	"net/http"
)

// Coordinates are accepted only as responses to an authorized location request.
// These are device-reported values, not proof of physical presence. Never use for attendance/security.
type SharedLocation struct {
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Accuracy    float64 `json:"accuracy_m"`
	CapturedAt  int64   `json:"captured_at"`
	Approximate bool    `json:"approximate"`
}

func validLocation(p SharedLocation, at int64) bool {
	for _, n := range []float64{p.Latitude, p.Longitude, p.Accuracy} {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return false
		}
	}
	return p.Latitude >= -90 && p.Latitude <= 90 && p.Longitude >= -180 && p.Longitude <= 180 &&
		p.Accuracy > 0 && p.Accuracy <= 20000 && p.CapturedAt >= at-120 && p.CapturedAt <= at+15
}

// 구형 앱이 직접 위치 전달 API를 호출해도 좌표를 저장하지 않습니다.
func (a *App) sendLocation(w http.ResponseWriter, r *http.Request, u User) {
	fail(w, apiError{410, "직접 위치 보내기는 삭제되었습니다. 친구 프로필의 현재 위치 확인을 사용하세요."})
}
