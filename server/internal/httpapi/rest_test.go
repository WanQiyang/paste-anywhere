package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paste-anywhere/server/internal/storage"
)

func TestRESTResourcesAndDeviceAuthorization(t *testing.T) {
	s, err := storage.Open(filepath.Join(t.TempDir(), "rest.db"), 20, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, invite, err := s.CreateUser("Alice")
	if err != nil {
		t.Fatal(err)
	}
	_, inviteB, _ := s.CreateUser("Bob")
	_, _, tokenB, _ := s.Connect(inviteB, "PC")
	handler := New(s, 1024, 4096).Handler()
	request := func(method, path, token, contentType string, body []byte, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d, want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		return w
	}
	createDevice := []byte(fmt.Sprintf(`{"invite":%q,"deviceName":"PC"}`, invite))
	request("POST", "/v1/devices", "", "text/plain", createDevice, 415)
	created := request("POST", "/v1/devices", "", "application/json", createDevice, 201)
	if created.Header().Get("Location") != "/v1/devices/Alice-PC" {
		t.Fatal("missing device resource location")
	}
	var connection struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &connection); err != nil || connection.Token == "" {
		t.Fatal("missing credential", err)
	}
	token := connection.Token
	_, _, phoneToken, _ := s.Connect(invite, "Phone")
	request("DELETE", "/v1/devices/Alice-Phone", token, "", nil, 403)
	request("DELETE", "/v1/devices/Bob-PC", token, "", nil, 403)
	request("DELETE", "/v1/devices/alice-PC", token, "", nil, 403)
	request("DELETE", "/v1/devices/current", token, "", nil, 400)
	if _, err := s.Authenticate(phoneToken); err != nil {
		t.Fatal("another device was revoked", err)
	}
	if _, err := s.Authenticate(tokenB); err != nil {
		t.Fatal("another user's device was revoked", err)
	}

	text := "hello\n中文 <b>content</b>"
	payload := []byte(fmt.Sprintf(`{"text":%q}`, text))
	for _, contentType := range []string{"", "application/octet-stream", "text/plain", "image/webp"} {
		request("POST", "/v1/entries", token, contentType, payload, 415)
	}
	textResult := request("POST", "/v1/entries", token, "application/json; charset=utf-8", payload, 201)
	var entry storage.Entry
	if err := json.Unmarshal(textResult.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if textResult.Header().Get("Location") != "/v1/entries/"+entry.ID {
		t.Fatal("wrong entry resource location")
	}
	var listed []storage.Entry
	if err := json.Unmarshal(request("GET", "/v1/entries?limit=1", token, "", nil, 200).Body.Bytes(), &listed); err != nil || len(listed) != 1 || listed[0].ID != entry.ID {
		t.Fatal("latest entry query failed", err)
	}
	raw := request("GET", "/v1/entries/"+entry.ID+"/content", token, "", nil, 200)
	if raw.Body.String() != text || raw.Header().Get("Content-Type") != "text/plain; charset=utf-8" || raw.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("text content representation incorrect")
	}
	request("GET", "/v1/entries/"+entry.ID+"/content", tokenB, "", nil, 404)
	request("POST", "/v1/transfers", token, "application/json", []byte(`{"userId":"2"}`), 400)
	request("POST", "/v1/transfers", tokenB, "application/json", []byte(fmt.Sprintf(`{"entryId":%q,"userId":"2"}`, entry.ID)), 404)
	r := httptest.NewRequest(http.MethodGet, "/v1/entries/"+entry.ID+"/content", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Range", "bytes=0-4")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != "hello" {
		t.Fatal("text content range failed", w.Code)
	}

	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	request("POST", "/v1/entries", token, "image/jpeg", picture.Bytes(), 415)
	imageResult := request("POST", "/v1/entries", token, "image/png", picture.Bytes(), 201)
	if err := json.Unmarshal(imageResult.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	raw = request("GET", "/v1/entries/"+entry.ID+"/content", token, "", nil, 200)
	if !bytes.Equal(raw.Body.Bytes(), picture.Bytes()) || raw.Header().Get("Content-Type") != "image/png" {
		t.Fatal("image content representation incorrect")
	}
	users := request("GET", "/v1/users", token, "", nil, 200).Body.String()
	if strings.Contains(users, `"online":`) || !strings.Contains(users, `"onlineDevices":0`) {
		t.Fatal("redundant/missing presence fields", users)
	}

	for _, old := range []struct{ method, path string }{
		{"POST", "/v1/connect"}, {"DELETE", "/v1/device"}, {"POST", "/v1/images"},
		{"GET", "/v1/clipboard"}, {"DELETE", "/v1/clipboard"}, {"GET", "/v1/history"},
		{"GET", "/v1/entries/1/image"}, {"POST", "/v1/entries/1/send"},
	} {
		request(old.method, old.path, token, "application/json", nil, 404)
	}
	request("DELETE", "/v1/devices/Alice-PC", token, "", nil, 204)
	request("GET", "/v1/users", token, "", nil, 401)
	request("GET", "/v1/users", phoneToken, "", nil, 200)
	request("GET", "/v1/users", tokenB, "", nil, 200)
}

// Clients may discard local credentials on 401 during logout. A storage outage
// must remain a server error, rather than falsely revoking a valid identity.
func TestStorageFailureIsNotInvalidCredential(t *testing.T) {
	s, err := storage.Open(filepath.Join(t.TempDir(), "unavailable.db"), 20, time.Hour)
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
	handler := New(s, 1024, 4096).Handler()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/v1/users"}, {"DELETE", "/v1/devices/Alice-PC"},
	} {
		r := httptest.NewRequest(endpoint.method, endpoint.path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("%s: storage failure returned %d", endpoint.path, w.Code)
		}
	}
}
