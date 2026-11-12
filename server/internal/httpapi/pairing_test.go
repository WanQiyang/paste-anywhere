package httpapi

import (
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paste-anywhere/server/internal/storage"
)

func TestShortPairingCode(t *testing.T) {
	s, err := storage.Open(filepath.Join(t.TempDir(), "pairing.db"), 20, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	user, invite, err := s.CreateUser("shortCode")
	if err != nil {
		t.Fatal(err)
	}
	checkCode := func(code string) {
		t.Helper()
		if len(code) != 8 || strings.Trim(code, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567") != "" {
			t.Fatalf("unexpected pairing code format: %q", code)
		}
	}
	checkCode(invite)
	handler := New(s, 1024, 16*1024*1024).Handler()
	connect := func(code string, want int) {
		t.Helper()
		r := httptest.NewRequest("POST", "/v1/devices", strings.NewReader(fmt.Sprintf(`{"invite":%q,"deviceName":"test"}`, code)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("connect status %d, want %d: %s", w.Code, want, w.Body.String())
		}
	}
	connect(invite, 201)
	connect(invite, 409)
	for _, name := range []string{"bad-name", "中文", "bad name", "", strings.Repeat("a", 129)} {
		r := httptest.NewRequest("POST", "/v1/devices", strings.NewReader(fmt.Sprintf(`{"invite":%q,"deviceName":%q}`, invite, name)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("invalid device name %q: %d", name, w.Code)
		}
	}
	_, _, token, err := s.Connect(invite, "savedDevice")
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 64 {
		t.Fatal("device token length changed")
	}
	err = s.DeleteUserByName(user.Name)
	if err != nil {
		t.Fatal(err)
	}
	_, reset, err := s.CreateUser(user.Name)
	if err != nil {
		t.Fatal(err)
	}
	checkCode(reset)
	connect(reset, 201)
	connect(invite, 401)
	if _, err := s.Authenticate(token); err == nil {
		t.Fatal("recreating user did not revoke device token")
	}
	connect("1234567", 400)
	connect("123456789", 400)
	connect(strings.Repeat("a", 64), 400)
	if _, _, _, err := s.Connect(strings.Repeat("a", 64), "legacy"); err == nil {
		t.Fatal("legacy pairing code accepted")
	}
}
