package storage

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestResetPreservesUserAndContentAndRollsBackOnFailure(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "reset.db"), 20, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, invite, err := s.CreateUser("Alice")
	if err != nil {
		t.Fatal(err)
	}
	d, _, token, err := s.Connect(invite, "PC")
	if err != nil {
		t.Fatal(err)
	}
	phone, _, phoneToken, err := s.Connect(invite, "Phone")
	if err != nil {
		t.Fatal(err)
	}
	_, otherInvite, err := s.CreateUser("Bob")
	if err != nil {
		t.Fatal(err)
	}
	other, _, otherToken, err := s.Connect(otherInvite, "PC")
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.CreateEntry(d, u.ID, "keep this content")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePresence([]string{d.ID, phone.ID, other.ID}); err != nil {
		t.Fatal(err)
	}
	before, _, _, err := s.ManagementLists()
	if err != nil {
		t.Fatal(err)
	}
	// A failed revocation must not leave an undisclosed new pairing code.
	if _, err := s.db.Exec(`CREATE TRIGGER reject_revoke BEFORE DELETE ON devices BEGIN SELECT RAISE(ABORT,'cannot revoke'); END`); err != nil {
		t.Fatal(err)
	}
	if code, err := s.ResetPairingCode(u.Name); err == nil || code != "" {
		t.Fatal("failed reset returned a code", err)
	}
	if _, err := s.Authenticate(token); err != nil {
		t.Fatal("failed reset revoked credentials", err)
	}
	if _, err := s.db.Exec("DROP TRIGGER reject_revoke"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.Connect(invite, "Tablet"); err != nil {
		t.Fatal("failed reset changed the code", err)
	}
	code, err := s.ResetPairingCode(u.Name)
	if err != nil || code == invite {
		t.Fatal("reset did not replace the code", err)
	}
	for _, credential := range []string{token, phoneToken} {
		if _, err := s.Authenticate(credential); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("reset device still authenticated", err)
		}
	}
	if _, err := s.Authenticate(otherToken); err != nil {
		t.Fatal("reset affected another user", err)
	}
	after, devices, _, err := s.ManagementLists()
	if err != nil || len(after) != 2 || len(devices) != 1 || devices[0].User != "Bob" {
		t.Fatal("unexpected reset scope", err)
	}
	if after[0].CreatedAt != before[0].CreatedAt || after[0].Devices != 0 || after[0].Online != 0 {
		t.Fatal("reset changed creation time or left paired devices")
	}
	if kept, err := s.Entry(u.ID, e.ID); err != nil || kept.Text != e.Text {
		t.Fatal("reset deleted content", err)
	}
	if _, _, _, err := s.Connect(invite, "PC"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("previous code still accepted", err)
	}
	if _, pairedUser, _, err := s.Connect(code, "PC"); err != nil || pairedUser.ID != u.ID {
		t.Fatal("reset replaced user identity or failed to release device name", err)
	}
	if _, err := s.ResetPairingCode("Missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("missing user accepted", err)
	}
	if _, err := s.ResetPairingCode("bad-name"); !errors.Is(err, ErrInvalidName) {
		t.Fatal("invalid name accepted", err)
	}
}
