package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestManagementSortingPresenceAndRetention(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "management.db"), 20, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var devices []Device
	for _, name := range []string{"bob", "alice", "Alice"} {
		_, code, err := s.CreateUser(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, deviceName := range []string{"PC2", "PC10"} {
			d, _, _, err := s.Connect(code, deviceName)
			if err != nil {
				t.Fatal(err)
			}
			devices = append(devices, d)
		}
	}
	d := devices[0]
	if err := s.RecordActivity(d.ID, "Windows", "0.2.1", "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	e, err := s.CreateEntry(d, d.UserID, "recent")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePresence([]string{d.ID, d.ID}); err != nil {
		t.Fatal(err)
	}
	users, list, known, err := s.ManagementLists()
	if err != nil || !known || len(users) != 3 || len(list) != 6 {
		t.Fatal("management snapshot unavailable", err)
	}
	if users[0].Name != "Alice" || users[1].Name != "alice" || users[2].Name != "bob" || list[0].Name != "PC10" || list[1].Name != "PC2" {
		t.Fatal("unstable name ordering", users, list)
	}
	if users[2].Devices != 2 || users[2].Online != 1 || users[2].RecentContent != e.CreatedAt || users[0].RecentContent != 0 {
		t.Fatal("incorrect counts or content times", users)
	}
	last := list[5]
	if !last.Online || last.Platform != "Windows" || last.Version != "0.2.1" || last.IP != "192.0.2.1" || last.CreatedAt == 0 || last.LastActive == 0 {
		t.Fatal("missing device metadata", last)
	}
	if _, err := s.db.Exec("UPDATE entries SET created_at=?", time.Now().Add(-2*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE server_status SET updated_at=?", time.Now().Add(-time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	users, _, known, err = s.ManagementLists()
	if err != nil || known || users[2].RecentContent != 0 {
		t.Fatal("stale presence or expired content shown", err)
	}
	if err := s.SavePresence(nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearPresence(); err != nil {
		t.Fatal(err)
	}
	_, _, known, err = s.ManagementLists()
	if err != nil || known {
		t.Fatal("stopped server reported known presence", err)
	}
}
