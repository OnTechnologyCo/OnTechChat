package server

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestKoreanUsernamePolicyCases(t *testing.T) {
	cases := []struct {
		name, value string
		ok          bool
	}{
		{"reported name", "나도윤", true}, {"Hangul and ASCII", "도윤abc_01.-", true},
		{"existing ASCII", "member01", true}, {"40 Hangul characters", strings.Repeat("가", 40), true},
		{"two characters", "도윤", false}, {"41 Hangul characters", strings.Repeat("가", 41), false},
		{"space", "나 도윤", false}, {"newline", "나도윤\n", false}, {"emoji", "나도윤😀", false},
		{"path separator", "나/도윤", false}, {"HTML", "<나도윤>", false},
		{"invisible separator", "나\u200b도윤", false}, {"Latin lookalike", "аdmin", false},
		{"decomposed jamo", "나도윤", false}, {"invalid UTF8", string([]byte{255, 255, 255}), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if usernamePattern.MatchString(c.value) != c.ok {
				t.Fatal("username policy mismatch")
			}
		})
	}
}

// Real submitted password is intentionally not saved in source, tests or reports.
func TestReproduceOldASCIIOnlyUsernameRule(t *testing.T) {
	old := regexp.MustCompile(`^[a-z0-9_.-]{3,40}$`)
	if old.MatchString("나도윤") {
		t.Fatal("baseline reproduction failed")
	}
	if !validNewPassword("!abcdeFg7") {
		t.Fatal("9-character synthetic password should pass")
	}
	if !usernamePattern.MatchString("나도윤") {
		t.Fatal("new username policy should pass")
	}
}

func TestKoreanUserCreateLoginInviteAndPermissions(t *testing.T) {
	_, s, _, _, _ := testApp(t)
	admin := loginTest(t, s, "admin")
	bob := loginTest(t, s, "bob")
	synthetic := "!abcdeFg7"
	status, b, _ := request(t, s, admin, "POST", "/api/users", map[string]string{"username": "  나도윤  ", "password": synthetic})
	expectStatus(t, status, http.StatusCreated, b)
	v := decodeMap(t, b)
	if v["username"] != "나도윤" {
		t.Fatal("username changed")
	}
	uid := v["id"].(string)
	status, b, _ = request(t, s, "", "POST", "/api/login", map[string]string{"username": "나도윤", "password": synthetic, "client": "native"})
	expectStatus(t, status, 200, b)
	token := decodeMap(t, b)["token"].(string)
	status, b, _ = request(t, s, admin, "POST", "/api/users", map[string]string{"username": "나도윤", "password": synthetic})
	expectStatus(t, status, 409, b)
	if !strings.Contains(string(b), "이미 사용") {
		t.Fatal("duplicate error not explicit")
	}
	status, b, _ = request(t, s, bob, "POST", "/api/users", map[string]string{"username": "일반사용자", "password": synthetic})
	expectStatus(t, status, 403, b)
	status, b, _ = request(t, s, admin, "POST", "/api/rooms", map[string]any{"name": "한글 아이디 검증", "members": []string{" 나도윤 "}})
	expectStatus(t, status, 201, b)
	room := decodeMap(t, b)["id"].(string)
	status, b, _ = request(t, s, token, "GET", "/api/rooms/"+room+"/members", nil)
	expectStatus(t, status, 200, b)
	if !strings.Contains(string(b), "나도윤") {
		t.Fatal("member name missing")
	}
	status, b, _ = request(t, s, admin, "DELETE", "/api/rooms/"+room+"/members/"+uid, nil)
	expectStatus(t, status, 200, b)
	status, b, _ = request(t, s, token, "GET", "/api/rooms/"+room+"/messages", nil)
	expectStatus(t, status, 403, b)
	status, b, _ = request(t, s, admin, "POST", "/api/rooms/"+room+"/members", map[string]string{"username": "나도윤"})
	expectStatus(t, status, 200, b)
	status, b, _ = request(t, s, token, "GET", "/api/rooms/"+room+"/messages", nil)
	expectStatus(t, status, 200, b)
	status, b, _ = request(t, s, bob, "GET", "/api/rooms/"+authRoomID(uid)+"/messages", nil)
	expectStatus(t, status, 403, b)
	status, b, _ = request(t, s, token, "GET", "/api/rooms/"+authRoomID(uid)+"/messages", nil)
	expectStatus(t, status, 200, b)
}

func TestCreateUserErrorsIdentifyTheActualField(t *testing.T) {
	_, s, _, _, _ := testApp(t)
	admin := loginTest(t, s, "admin")
	status, b, _ := request(t, s, admin, "POST", "/api/users", map[string]string{"username": "나 도윤", "password": "!abcdeFg7"})
	expectStatus(t, status, 400, b)
	errMsg := decodeMap(t, b)["error"].(string)
	if !strings.Contains(errMsg, "아이디") || strings.Contains(errMsg, "비밀번호") {
		t.Fatal("invalid username misreported as password error")
	}
	status, b, _ = request(t, s, admin, "POST", "/api/users", map[string]string{"username": "나도윤", "password": "short7!"})
	expectStatus(t, status, 400, b)
	errMsg = decodeMap(t, b)["error"].(string)
	if !strings.Contains(errMsg, "비밀번호") || strings.Contains(errMsg, "아이디") {
		t.Fatal("invalid password error not distinct")
	}
	status, b, _ = request(t, s, "", "GET", "/healthz", nil)
	expectStatus(t, status, 200, b)
	health := decodeMap(t, b)
	if health["version"] != version || health["password_min_chars"] != float64(8) || health["supports_korean_usernames"] != true {
		t.Fatal("health policy/version mismatch")
	}
}

func TestKoreanMailRecipientConfig(t *testing.T) {
	c := defaults()
	c.Mail.OwnerUsername = "나도윤"
	c.Mail.RecipientUsernames = []string{"나도윤", "member01"}
	if e := c.validate(); e != nil {
		t.Fatal("Korean recipient rejected", e)
	}
	c.Mail.RecipientUsernames = []string{"나도윤", "나도윤"}
	if e := c.validate(); e == nil {
		t.Fatal("duplicate recipient allowed")
	}
	c.Mail.RecipientUsernames = []string{"나 도윤"}
	if e := c.validate(); e == nil {
		t.Fatal("invalid recipient allowed")
	}
}
