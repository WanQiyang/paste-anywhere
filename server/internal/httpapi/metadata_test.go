package httpapi

import (
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"paste-anywhere/server/internal/storage"
)

func TestDeviceMetadataAndProxyTrust(t *testing.T) {
	s, err := storage.Open(filepath.Join(t.TempDir(), "metadata.db"), 20, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, invite, err := s.CreateUser("Alice")
	if err != nil {
		t.Fatal(err)
	}
	_, _, token, err := s.Connect(invite, "PC")
	if err != nil {
		t.Fatal(err)
	}
	api := New(s, 1024, 4096)
	for _, tc := range []struct {
		trust           bool
		forwarded, want string
	}{
		{false, "203.0.113.10", "192.0.2.1"},
		{true, "203.0.113.10", "203.0.113.10"},
		{true, "not-an-ip", "192.0.2.1"},
	} {
		api.TrustProxy = tc.trust
		r := httptest.NewRequest("GET", "/v1/users", nil)
		r.RemoteAddr = "192.0.2.1:4321"
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Real-IP", tc.forwarded)
		r.Header.Set("X-Paste-Platform", "Windows")
		r.Header.Set("X-Paste-Version", "0.2.1")
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		_, devices, _, err := s.ManagementLists()
		if err != nil || len(devices) != 1 {
			t.Fatal("device metadata unavailable", err)
		}
		d := devices[0]
		if d.IP != tc.want || d.Platform != "Windows" || d.Version != "0.2.1" || d.LastActive == 0 {
			t.Fatalf("trust=%t: unexpected metadata %+v", tc.trust, d)
		}
	}
}
