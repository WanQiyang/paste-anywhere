package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paste-anywhere/server/internal/storage"
)

func TestManagementCLI(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cli.db")
	t.Setenv("PASTE_ANYWHERE_DB", db)
	run := func(args ...string) string {
		t.Helper()
		var out, diagnostics bytes.Buffer
		if err := runCLI(args, &out, &diagnostics); err != nil {
			t.Fatalf("%v: %v %s", args, err, diagnostics.String())
		}
		return out.String()
	}
	var account struct {
		User   struct{ Name string }
		Invite string
	}
	created := run("user", "create", "ribo")
	if err := json.Unmarshal([]byte(created), &account); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(created, `"id"`) || account.User.Name != "ribo" || len(account.Invite) != 8 {
		t.Fatal(created)
	}
	if result := run("user", "list"); result != "[\"ribo\"]\n" {
		t.Fatal(result)
	}
	s, err := storage.Open(db, 20, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, _, token, err := s.Connect(account.Invite, "PC")
	if err != nil {
		t.Fatal(err)
	}
	if result := run("device", "list"); result != "[{\"name\":\"ribo-PC\",\"user\":\"ribo\",\"device\":\"PC\"}]\n" {
		t.Fatal(result)
	}
	run("device", "delete", "ribo-PC")
	if _, err := s.Authenticate(token); err == nil {
		t.Fatal("deleted device still authenticated")
	}
	_, _, token, err = s.Connect(account.Invite, "PC")
	if err != nil {
		t.Fatal(err)
	}
	run("user", "delete", "ribo")
	if _, err := s.Authenticate(token); err == nil {
		t.Fatal("deleted user's device still authenticated")
	}
	if result := run("device", "list"); result != "[]\n" {
		t.Fatal(result)
	}
	if result := run("user", "list"); result != "[]\n" {
		t.Fatal(result)
	}
	for _, args := range [][]string{{"user", "delete", "ribo"}, {"device", "delete", "ribo-PC"}} {
		if err := runCLI(args, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("missing target accepted: %v", args)
		}
	}
	// Flags are accepted on either side of the positional name and override env.
	other := filepath.Join(t.TempDir(), "other.db")
	run("user", "create", "Other", "--db", other)
	run("user", "create", "--db="+other, "Other2")
	if result := run("user", "list", "--db", other); result != "[\"Other\",\"Other2\"]\n" {
		t.Fatal(result)
	}
	if result := run("user", "list"); result != "[]\n" {
		t.Fatal("env database changed", result)
	}
}

func TestInvalidCLIHasNoDatabaseSideEffects(t *testing.T) {
	db := filepath.Join(t.TempDir(), "not-created.db")
	t.Setenv("PASTE_ANYWHERE_DB", db)
	for _, args := range [][]string{
		{"user-create"}, {"user-reset"}, {"install"}, {"uninstall"}, {"user", "reset-code", "ribo"},
		{"user", "create"}, {"user", "create", "bad-name"}, {"user", "create", "中文"},
		{"device", "delete", "PC"}, {"device", "list", "extra"},
		{"device", "delete", "ribo-PC-old"}, {"device", "delete", "ribo-中文"}, {"user", "delete", "旧用户"},
		{"user", "create", "ribo", "--unknown", "value"}, {"serve", "--db"},
		{"serve", "--max-text", "0"},
	} {
		if err := runCLI(args, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted invalid command: %v", args)
		}
	}
	for _, args := range [][]string{nil, {"--help"}, {"serve", "--help"}, {"user", "create", "--help"}, {"device", "--help"}} {
		if err := runCLI(args, &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Fatalf("invalid/help command created database: %v", err)
	}
}
