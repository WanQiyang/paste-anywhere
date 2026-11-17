package httpapi

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paste-anywhere/server/internal/storage"
)

type pushRecorder struct{ tokens []string }

func (p *pushRecorder) Send(_ context.Context, token, _ string) error {
	p.tokens = append(p.tokens, token)
	return nil
}

func TestPushRegistrationRoutingAndRevocation(t *testing.T) {
	s, err := storage.Open(filepath.Join(t.TempDir(), "push.db"), 20, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u1, i1, _ := s.CreateUser("Alice")
	u2, i2, _ := s.CreateUser("Bob")
	source, _, sourceKey, _ := s.Connect(i1, "PC")
	phone, _, phoneKey, _ := s.Connect(i1, "Phone")
	other, _, otherKey, _ := s.Connect(i2, "Phone")
	a := New(s, 1024, 1024)
	a.pushQueue = make(chan pushJob, 128)
	handler := a.Handler()
	request := func(method, path, credential, body string, status int) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+credential)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	request("PUT", "/v1/push-token", "bad", `{"token":"x"}`, 401)
	request("PUT", "/v1/push-token", phoneKey, `{"token":"bad token"}`, 400)
	request("PUT", "/v1/push-token", phoneKey, `{}`, 400)
	request("PUT", "/v1/push-token", phoneKey, `{"token":null}`, 400)
	request("PUT", "/v1/push-token", phoneKey, `{"token":"x","userId":"2"}`, 400)
	request("PUT", "/v1/push-token", sourceKey, `{"token":"source"}`, 204)
	request("PUT", "/v1/push-token", phoneKey, `{"token":"phone"}`, 204)
	request("PUT", "/v1/push-token", otherKey, `{"token":"other"}`, 204)
	request("POST", "/v1/entries", sourceKey, `{"text":"private text"}`, 201)
	job := <-a.pushQueue
	if job.userID != u1.ID || len(job.targets) != 1 || job.targets[0].DeviceID != phone.ID {
		t.Fatalf("wrong recipients: %+v", job)
	}
	recorder := &pushRecorder{}
	a.deliverPush(context.Background(), recorder, job)
	if len(recorder.tokens) != 1 || recorder.tokens[0] != "phone" {
		t.Fatal(recorder.tokens)
	}
	request("POST", "/v1/transfers", sourceKey, `{"entryId":"`+job.entryID+`","userId":"`+u2.ID+`"}`, 201)
	forward := <-a.pushQueue
	if len(forward.targets) != 1 || forward.targets[0].DeviceID != other.ID {
		t.Fatalf("wrong transfer: %+v", forward)
	}
	// Rebinding the same token removes the old account's route and invalidates queued work.
	request("PUT", "/v1/push-token", otherKey, `{"token":"phone"}`, 204)
	a.deliverPush(context.Background(), recorder, job)
	a.deliverPush(context.Background(), recorder, forward)
	if len(recorder.tokens) != 1 {
		t.Fatal("stale ownership delivered")
	}
	targets, _ := s.PushTargets(u1.ID, source.ID)
	if len(targets) != 0 {
		t.Fatal("duplicate token ownership")
	}
	request("PUT", "/v1/push-token", phoneKey, `{"token":"new-phone"}`, 204)
	request("POST", "/v1/entries", sourceKey, `{"text":"next"}`, 201)
	job = <-a.pushQueue
	if _, err = s.ClearClipboard(source); err != nil {
		t.Fatal(err)
	}
	a.deliverPush(context.Background(), recorder, job)
	if len(recorder.tokens) != 1 {
		t.Fatal("cleared entry delivered")
	}
	request("PUT", "/v1/push-token", phoneKey, `{"token":""}`, 204)
	targets, _ = s.PushTargets(u1.ID, source.ID)
	if len(targets) != 0 {
		t.Fatal("disable failed")
	}
	if _, err = s.ResetPairingCode("Bob"); err != nil {
		t.Fatal(err)
	}
	targets, _ = s.PushTargets(u2.ID, "")
	if len(targets) != 0 {
		t.Fatal("reset left push token")
	}
	if err = s.SetPushToken(other, "late"); err == nil {
		t.Fatal("revoked device registered token")
	}
}
