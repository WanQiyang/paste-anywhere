package storage

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestImageReferenceCleanupAndRollback(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "data.db"), 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, invite, _ := s.CreateUser("sender")
	other, _, _ := s.CreateUser("receiver")
	d, _, _, _ := s.Connect(invite, "phone")
	e, err := s.CreateImage(d, []byte("image bytes"), Attachment{Name: "image.png", MIME: "image/png", Width: 1, Height: 1})
	if err != nil {
		t.Fatal(err)
	}
	forwarded, err := s.Forward(d, e.ID, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateEntry(d, u.ID, "replace original"); err != nil {
		t.Fatal(err)
	}
	if err = s.Cleanup(); err != nil {
		t.Fatal(err)
	}
	f, _, err := s.OpenImage(other.ID, forwarded.ID)
	if err != nil {
		t.Fatal("recipient's reference was deleted", err)
	}
	f.Close()
	if _, _, err = s.OpenImage(u.ID, e.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("pruned entry still accessible", err)
	}
	path := filepath.Join(s.files, e.Attachment.ID)
	if err = s.DeleteUser(other.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("unreferenced attachment file remains", err)
	}
	if _, err = s.CreateImage(Device{ID: "revoked", UserID: u.ID}, []byte("orphan"), Attachment{}); err == nil {
		t.Fatal("revoked device created image")
	}
	files, err := os.ReadDir(s.files)
	if err != nil || len(files) != 0 {
		t.Fatal("failed upload left orphan file", err)
	}
}

func TestPersistenceExpiryAndIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	s, err := Open(path, 2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	u, invite, err := s.CreateUser("ribo")
	if err != nil {
		t.Fatal(err)
	}
	d, _, token, err := s.Connect(invite, "phone")
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.CreateEntry(d, u.ID, "persisted")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, 2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if restored, err := s.Authenticate(token); err != nil || restored.ID != d.ID {
		t.Fatal("device credential was not persisted", err)
	}
	if restored, err := s.Latest(u.ID); err != nil || restored.Text != "persisted" {
		t.Fatal("entry was not persisted", err)
	}
	// Move the persisted row outside the retention window without sleeping.
	if _, err = s.db.Exec("UPDATE entries SET created_at=?", time.Now().Add(-2*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Entry(u.ID, e.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("expired entry readable", err)
	}
	if err = s.Cleanup(); err != nil {
		t.Fatal(err)
	}
	next, err := s.CreateEntry(d, u.ID, "new")
	if err != nil {
		t.Fatal(err)
	}
	previousID, _ := strconv.ParseInt(e.ID, 10, 64)
	nextID, _ := strconv.ParseInt(next.ID, 10, 64)
	if nextID <= previousID {
		t.Fatal("global ID reused after cleanup")
	}
	if err = s.DeleteUser(u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(token); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("deleted user's device remains valid", err)
	}
}
