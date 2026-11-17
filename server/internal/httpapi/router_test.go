package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"paste-anywhere/server/internal/storage"
)

func TestNativeAndBrowserWebSocketOrigins(t *testing.T) {
	s, err := storage.Open(filepath.Join(t.TempDir(), "origin.db"), 2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, invite, err := s.CreateUser("originTest")
	if err != nil {
		t.Fatal(err)
	}
	_, _, token, err := s.Connect(invite, "phone")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(s, 1024, 16*1024*1024).Handler())
	defer server.Close()
	u, _ := url.Parse(server.URL)
	for _, test := range []struct {
		name, origin, credential string
		allowed                  bool
	}{
		{"native-no-origin-allowed", "", token, true},
		{"invalid-bare-host", u.Hostname(), token, false},
		{"invalid-bare-host-port", u.Host, token, false},
		{"harmony-origin-missing-port", "http://" + u.Hostname(), token, false},
		{"browser-same-origin", server.URL, token, true},
		{"native-other-host", "other.example", token, false},
		{"browser-other-origin", "https://other.example", token, false},
		{"browser-wrong-port", "http://" + u.Hostname() + ":1", token, false},
		{"unauthenticated-native", u.Hostname(), "invalid", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			header := http.Header{"Authorization": []string{"Bearer " + test.credential}, "Origin": []string{test.origin}}
			c, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/events", &websocket.DialOptions{HTTPHeader: header})
			if c != nil {
				defer c.CloseNow()
			}
			if response != nil && response.Body != nil {
				defer response.Body.Close()
			}
			if test.allowed {
				if err != nil {
					t.Fatal(err)
				}
				var event Event
				if err = wsjson.Read(ctx, c, &event); err != nil || event.Type != "ready" {
					t.Fatalf("not ready: %v %+v", err, event)
				}
			} else if err == nil {
				t.Fatal("unexpectedly accepted origin or credential")
			}
		})
	}
}

func TestIsolationDeliveryAndReconnect(t *testing.T) {
	s, err := storage.Open(filepath.Join(t.TempDir(), "test.db"), 2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u1, i1, _ := s.CreateUser("one")
	u2, i2, _ := s.CreateUser("two")
	d1, _, token1, _ := s.Connect(i1, "source")
	_, _, token2, _ := s.Connect(i1, "receiver")
	_, _, otherToken, _ := s.Connect(i2, "other")
	server := httptest.NewServer(New(s, 1024, 16*1024*1024).Handler())
	defer server.Close()
	request := func(method, path, token, body string, status int) json.RawMessage {
		t.Helper()
		r, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		resp, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		var data json.RawMessage
		if e = json.NewDecoder(resp.Body).Decode(&data); e != nil {
			t.Fatal(e)
		}
		if resp.StatusCode != status {
			t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, data)
		}
		return data
	}
	open := func(token string) *websocket.Conn {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		c, _, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/events", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
		if e != nil {
			t.Fatal(e)
		}
		var ready Event
		if e = wsjson.Read(ctx, c, &ready); e != nil || ready.Type != "ready" {
			t.Fatalf("ready: %v %+v", e, ready)
		}
		return c
	}
	read := func(c *websocket.Conn) Event {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var e Event
		if err := wsjson.Read(ctx, c, &e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	source := open(token1)
	defer source.CloseNow()
	receiver := open(token2)
	defer receiver.CloseNow()
	other := open(otherToken)
	defer other.CloseNow()
	var entry storage.Entry
	_ = json.Unmarshal(request("POST", "/v1/entries", token1, `{"text":"中文\nhello"}`, 201), &entry)
	notice := read(receiver)
	if notice.ItemID != entry.ID || notice.SourceDeviceID != d1.ID {
		t.Fatalf("unexpected notification: %+v", notice)
	}
	request("GET", "/v1/entries/"+entry.ID, otherToken, "", 404)
	if string(request("GET", "/v1/entries?limit=1", otherToken, "", 200)) != "[]" {
		t.Fatal("user isolation broken")
	}
	request("GET", "/v1/users", "", "", 401)
	request("POST", "/v1/entries", token1, `{"text":"`+strings.Repeat("x", 1025)+`"}`, 400)
	var sent storage.Entry
	_ = json.Unmarshal(request("POST", "/v1/transfers", token1, fmt.Sprintf(`{"entryId":%q,"userId":%q}`, entry.ID, u2.ID), 201), &sent)
	if sent.ID == entry.ID || sent.UserID != u2.ID || sent.SenderID != u1.ID {
		t.Fatal("forward must create a new recipient entry")
	}
	if read(other).ItemID != sent.ID {
		t.Fatal("recipient notification missing")
	}
	// A reconnect must not replay the forwarded item.
	other.CloseNow()
	reconnected := open(otherToken)
	defer reconnected.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	var event Event
	if err := wsjson.Read(ctx, reconnected, &event); err == nil {
		t.Fatalf("offline replay: %+v", event)
	}
	// Source device must never get its own upload or a send to another user.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel2()
	if err := wsjson.Read(ctx2, source, &event); err == nil {
		t.Fatalf("unexpected source update: %+v", event)
	}
	request("POST", "/v1/entries", token1, `{"text":"second"}`, 201)
	request("POST", "/v1/entries", token1, `{"text":"third"}`, 201)
	request("GET", "/v1/entries/"+entry.ID, token1, "", 404)
	if err = s.DeleteUserByName(u1.Name); err != nil {
		t.Fatal(err)
	}
	request("GET", "/v1/users", token1, "", 401)
	if _, _, _, err = s.Connect(i1, "oldInvite"); err == nil {
		t.Fatal("old invite accepted")
	}
}
