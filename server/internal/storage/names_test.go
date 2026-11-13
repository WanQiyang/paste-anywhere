package storage

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNameRulesAndDeviceOwnership(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "names.db"), 20, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, invite, err := s.CreateUser("ribo")
	if err != nil {
		t.Fatal(err)
	}
	_, other, err := s.CreateUser("Ribo") // Explicitly case-sensitive.
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateUser("ribo"); !errors.Is(err, ErrNameExists) {
		t.Fatal(err)
	}
	for _, name := range []string{"", "中", "a-b", "a_b", "a b", " abc", strings.Repeat("a", 129)} {
		if _, _, err := s.CreateUser(name); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("user %q: %v", name, err)
		}
		if _, _, _, err := s.Connect(invite, name); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("device %q: %v", name, err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _, _, err := s.Connect(invite, "PC1"); results <- err }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrNameExists) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("same-name concurrent pairing succeeded %d times", success)
	}
	_, _, token, err := s.Connect(other, "PC1")
	if err != nil {
		t.Fatal("different users cannot share device name", err)
	}
	if err := s.DeleteDeviceByName("ribo-PC1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(token); err != nil {
		t.Fatal("deleted other user's device", err)
	}
	if _, _, _, err := s.Connect(invite, "PC1"); err != nil {
		t.Fatal("deleted name cannot be reused", err)
	}
}

func TestDatabaseEnforcesNameConstraints(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "constraints.db"), 20, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, invite, err := s.CreateUser("ribo")
	if err != nil {
		t.Fatal(err)
	}
	d, _, _, err := s.Connect(invite, "PC")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "中文", "bad-name", "bad name", strings.Repeat("a", 129)} {
		if _, err := s.db.Exec("UPDATE users SET name=? WHERE id=?", name, u.ID); err == nil {
			t.Fatalf("database accepted user name %q", name)
		}
		if _, err := s.db.Exec("UPDATE devices SET name=? WHERE id=?", name, d.ID); err == nil {
			t.Fatalf("database accepted device name %q", name)
		}
		if err := s.DeleteUserByName(name); !errors.Is(err, ErrInvalidName) {
			t.Fatal(err)
		}
		if err := s.DeleteDeviceByName("ribo-" + name); !errors.Is(err, ErrInvalidName) {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec("INSERT INTO devices(id,user_id,name,token_hash,created_at) VALUES('other',?,'PC','other',?)", u.ID, time.Now().UnixMilli()); err == nil {
		t.Fatal("database accepted duplicate device name")
	}
}
