package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"paste-anywhere/server/internal/storage"
)

func TestClearClipboardIsolationNotificationsAndWatermark(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clear.db")
	s, err := storage.Open(path, 20, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, inviteA, _ := s.CreateUser("Alice")
	b, inviteB, _ := s.CreateUser("Bob")
	d, _, tokenA, _ := s.Connect(inviteA, "PC")
	_, _, tokenA2, _ := s.Connect(inviteA, "Phone")
	_, _, tokenB, _ := s.Connect(inviteB, "PC")
	for i := 0; i < 7; i++ {
		if _, err := s.CreateEntry(d, a.ID, "text"); err != nil {
			t.Fatal(err)
		}
	}
	shared, err := s.CreateImage(d, []byte("shared-image"), storage.Attachment{Name: "shared.png", MIME: "image/png", Width: 1, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateImage(d, []byte("private-image"), storage.Attachment{Name: "private.png", MIME: "image/png", Width: 1, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	forwarded, err := s.Forward(d, shared.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(s, 1024, 1024).Handler())
	defer server.Close()
	open := func(token string) *websocket.Conn {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/events", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
		if err != nil {
			t.Fatal(err)
		}
		var ready Event
		if err = wsjson.Read(ctx, c, &ready); err != nil || ready.Type != "ready" {
			t.Fatal("not ready", err)
		}
		return c
	}
	read := func(c *websocket.Conn) Event {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var event Event
		if err := wsjson.Read(ctx, c, &event); err != nil {
			t.Fatal(err)
		}
		return event
	}
	request := func(method, endpoint, token, body string, want int) []byte {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+endpoint, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != want {
			t.Fatalf("%s %s: %d %s %v", method, endpoint, response.StatusCode, data, err)
		}
		return data
	}
	c1, c2, other := open(tokenA), open(tokenA2), open(tokenB)
	defer c1.CloseNow()
	defer c2.CloseNow()
	defer other.CloseNow()
	request("DELETE", "/v1/entries", "", "", 401)
	var cleared Event
	if err := json.Unmarshal(request("DELETE", "/v1/entries", tokenA, "", 200), &cleared); err != nil {
		t.Fatal(err)
	}
	if cleared.Type != "clipboard.cleared" || cleared.SourceDeviceID != d.ID || cleared.ItemID != forwarded.ID {
		t.Fatalf("wrong clear watermark: %+v", cleared)
	}
	if got := read(c1); got != cleared {
		t.Fatal("source device missed clear", got)
	}
	if got := read(c2); got != cleared {
		t.Fatal("other device missed clear", got)
	}
	if raw := request("GET", "/v1/entries", tokenA, "", 200); strings.TrimSpace(string(raw)) != "[]" {
		t.Fatalf("history not fully cleared: %s", raw)
	}
	if raw := request("GET", "/v1/entries?limit=1", tokenA2, "", 200); strings.TrimSpace(string(raw)) != "[]" {
		t.Fatal("latest not cleared")
	}
	request("GET", "/v1/entries/"+shared.ID, tokenA, "", 404)
	request("GET", "/v1/entries/"+private.ID+"/content", tokenA, "", 404)
	if err := s.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path+".attachments", private.Attachment.ID)); !os.IsNotExist(err) {
		t.Fatal("unreferenced private image remains", err)
	}
	if data := request("GET", "/v1/entries/"+forwarded.ID+"/content", tokenB, "", 200); string(data) != "shared-image" {
		t.Fatal("another user's shared image changed")
	}
	var repeated Event
	if err := json.Unmarshal(request("DELETE", "/v1/entries", tokenA, "", 200), &repeated); err != nil || repeated.ItemID != cleared.ItemID {
		t.Fatal("empty clear changed watermark", err)
	}
	read(c1)
	read(c2)
	var fresh storage.Entry
	if err := json.Unmarshal(request("POST", "/v1/entries", tokenA, `{"text":"after-clear"}`, 201), &fresh); err != nil {
		t.Fatal(err)
	}
	cutoff, _ := strconv.ParseInt(cleared.ItemID, 10, 64)
	next, _ := strconv.ParseInt(fresh.ID, 10, 64)
	if next <= cutoff {
		t.Fatal("IDs reused after clear")
	}
	if event := read(c2); event.Type != "clipboard.updated" || event.ItemID != fresh.ID {
		t.Fatal("new content did not follow clear", event)
	}
	request("DELETE", "/v1/devices/Alice-PC", tokenA, "", 204)
	request("DELETE", "/v1/entries", tokenA, "", 401)
	if _, err := s.ClearClipboard(d); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("revoked device could clear", err)
	}
	if latest, err := s.Latest(a.ID); err != nil || latest.ID != fresh.ID {
		t.Fatal("fresh content lost", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	var unexpected Event
	if err := wsjson.Read(ctx, other, &unexpected); err == nil {
		t.Fatal("clear leaked to other user", unexpected)
	}
}
