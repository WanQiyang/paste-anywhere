package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestPresenceAndLogout(t *testing.T) {
	s, err := storage.Open(filepath.Join(t.TempDir(), "presence.db"), 20, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	alice, invite, _ := s.CreateUser("Alice")
	bob, otherInvite, _ := s.CreateUser("Bob")
	_, _, tokenA, _ := s.Connect(invite, "PC")
	_, _, tokenA2, _ := s.Connect(invite, "Phone")
	_, _, tokenB, _ := s.Connect(otherInvite, "PC")
	server := httptest.NewServer(New(s, 1024, 1024).Handler())
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
		if err := wsjson.Read(ctx, conn, &ready); err != nil || ready.Type != "ready" {
			t.Fatalf("ready: %v %+v", err, ready)
		}
		return conn
	}
	request := func(method, path, token string, want int) []PresenceUser {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("%s %s: got %d, want %d", method, path, response.StatusCode, want)
		}
		var users []PresenceUser
		if want == 200 {
			if err := json.NewDecoder(response.Body).Decode(&users); err != nil {
				t.Fatal(err)
			}
		}
		return users
	}
	waitPresence := func(aDevices, bDevices int) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			users := request("GET", "/v1/users", tokenB, 200)
			if len(users) == 2 && users[0].ID == alice.ID && users[1].ID == bob.ID &&
				users[0].OnlineDevices == aDevices && users[1].OnlineDevices == bDevices {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("unexpected presence: %+v", users)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitPresence(0, 0)
	a1, a2, b := open(tokenA), open(tokenA2), open(tokenB)
	defer a1.CloseNow()
	defer a2.CloseNow()
	defer b.CloseNow()
	waitPresence(2, 1)
	duplicate := open(tokenA)
	defer duplicate.CloseNow()
	waitPresence(2, 1) // Multiple sockets for one device count only once.
	request("DELETE", "/v1/devices/Alice-PC", "", 401)
	request("DELETE", "/v1/devices/Alice-Phone", tokenA, 403)
	request("DELETE", "/v1/devices/Bob-PC", tokenA, 403)
	request("DELETE", "/v1/devices/Alice-PC", tokenA, 204)
	request("GET", "/v1/users", tokenA, 401)
	if _, err := s.Authenticate(tokenA2); err != nil {
		t.Fatal("logout revoked another device", err)
	}
	waitPresence(1, 1) // Another device keeps Alice online.
	a2.CloseNow()
	waitPresence(0, 1)
	_, _, replacement, err := s.Connect(invite, "PC")
	if err != nil {
		t.Fatal("logout did not release device name", err)
	}
	newConnection := open(replacement)
	defer newConnection.CloseNow()
	waitPresence(1, 1)
	request("DELETE", "/v1/devices/Alice-PC", replacement, 204)
	waitPresence(0, 1)
}

func TestDashboardHistoryAndSendToSelf(t *testing.T) {
	s, err := storage.Open(filepath.Join(t.TempDir(), "dashboard.db"), 20, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	user, invite, _ := s.CreateUser("Alice")
	d, _, token, _ := s.Connect(invite, "PC")
	for i := 0; i < 7; i++ {
		if _, err := s.CreateEntry(d, user.ID, fmt.Sprintf("item%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	handler := New(s, 1024, 1024).Handler()
	request := func(method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w
	}
	var items []storage.Entry
	if err := json.Unmarshal(request("GET", "/v1/entries?limit=5", "", 200).Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 || items[0].Text != "item6" || items[4].Text != "item2" {
		t.Fatalf("wrong dashboard entries: %+v", items)
	}
	request("POST", "/v1/transfers", fmt.Sprintf(`{"entryId":%q,"userId":%q}`, items[4].ID, user.ID), 201)
	latest, err := s.Latest(user.ID)
	if err != nil || latest.Text != "item2" || latest.ID == items[4].ID {
		t.Fatalf("sending history to self failed: %+v %v", latest, err)
	}
	for _, value := range []string{"0", "-1", "invalid"} {
		request("GET", "/v1/entries?limit="+value, "", 400)
	}
	s.HistoryLimit = 2
	if err := json.Unmarshal(request("GET", "/v1/entries?limit=5", "", 200).Body.Bytes(), &items); err != nil || len(items) != 2 {
		t.Fatalf("configured history limit ignored: %+v %v", items, err)
	}
	request("DELETE", "/v1/devices/Alice-PC", "", 204)
	latest, err = s.Latest(user.ID)
	if err != nil || latest.Text != "item2" {
		t.Fatal("logout deleted clipboard history", err)
	}
}
