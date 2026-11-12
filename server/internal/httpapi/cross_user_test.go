package httpapi

import (
	"bytes"
	"context"
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

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"paste-anywhere/server/internal/storage"
)

func TestCrossUserRoundTrip(t *testing.T) {
	for _, kind := range []string{"text", "image"} {
		t.Run(kind, func(t *testing.T) {
			s, err := storage.Open(filepath.Join(t.TempDir(), "roundtrip.db"), 20, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			a, inviteA, _ := s.CreateUser("A")
			b, inviteB, _ := s.CreateUser("B")
			deviceA, _, tokenA, _ := s.Connect(inviteA, "phone")
			deviceB, _, tokenB, _ := s.Connect(inviteB, "desktop")
			_, _, tokenB2, _ := s.Connect(inviteB, "second desktop")
			old, err := s.CreateEntry(deviceB, b.ID, "existing recipient content")
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(New(s, 1024, 1024*1024).Handler())
			defer server.Close()
			open := func(token string) *websocket.Conn {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/events", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
				if err != nil {
					t.Fatal(err)
				}
				var ready Event
				if err = wsjson.Read(ctx, conn, &ready); err != nil || ready.Type != "ready" {
					t.Fatal("subscription not ready", err)
				}
				return conn
			}
			read := func(conn *websocket.Conn, id, source string) {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				var event Event
				if err := wsjson.Read(ctx, conn, &event); err != nil {
					t.Fatal(err)
				}
				if event.ItemID != id || event.SourceDeviceID != source {
					t.Fatalf("wrong recipient/update: %+v", event)
				}
			}
			post := func(token, path string, body []byte) storage.Entry {
				t.Helper()
				req, _ := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+token)
				response, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				if response.StatusCode != 201 {
					t.Fatalf("POST %s returned %d", path, response.StatusCode)
				}
				var entry storage.Entry
				if err = json.NewDecoder(response.Body).Decode(&entry); err != nil {
					t.Fatal(err)
				}
				return entry
			}
			ca, cb, cb2 := open(tokenA), open(tokenB), open(tokenB2)
			defer ca.CloseNow()
			defer cb.CloseNow()
			defer cb2.CloseNow()
			path, body := "/v1/entries", []byte(`{"text":"跨用户往返测试"}`)
			if kind == "image" {
				var pngData bytes.Buffer
				if err = png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
					t.Fatal(err)
				}
				path, body = "/v1/images", pngData.Bytes()
			}
			original := post(tokenA, path, body)
			toB := post(tokenA, "/v1/entries/"+original.ID+"/send", []byte(fmt.Sprintf(`{"userId":%q}`, b.ID)))
			// These must be the first notifications: A's ordinary upload must not leak to B.
			read(cb, toB.ID, deviceA.ID)
			read(cb2, toB.ID, deviceA.ID)
			latest, err := s.Latest(a.ID)
			if err != nil || latest.ID != original.ID {
				t.Fatal("sending overwrote sender's clipboard", err)
			}
			history, err := s.History(b.ID)
			if err != nil || len(history) != 2 || history[1].ID != old.ID {
				t.Fatal("recipient history was lost", err)
			}
			back := post(tokenB, "/v1/entries/"+toB.ID+"/send", []byte(fmt.Sprintf(`{"userId":%q}`, a.ID)))
			// A must not receive its own original upload, nor its send to B, before this reply.
			read(ca, back.ID, deviceB.ID)
			if back.ID == original.ID || back.ID == toB.ID || back.SenderID != b.ID || back.Kind != kind {
				t.Fatalf("invalid return entry: %+v", back)
			}
			if kind == "image" && (back.Attachment == nil || back.Attachment.ID != original.Attachment.ID) {
				t.Fatal("image attachment not reused")
			}
			latest, err = s.Latest(b.ID)
			if err != nil || latest.ID != toB.ID {
				t.Fatal("return send overwrote B's clipboard", err)
			}
		})
	}
}
