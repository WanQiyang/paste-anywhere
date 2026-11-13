package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paste-anywhere/server/internal/storage"
)

func TestTimestampOffsets(t *testing.T) {
	for _, tc := range []struct{ zone, utc, want string }{
		{"Asia/Shanghai", "2026-01-01T00:00:00Z", "2026-01-01 08:00:00 (+8)"},
		{"Asia/Kolkata", "2026-01-01T00:00:00Z", "2026-01-01 05:30:00 (+5:30)"},
		{"America/St_Johns", "2026-01-01T00:00:00Z", "2025-12-31 20:30:00 (-3:30)"},
		{"America/New_York", "2026-01-01T00:00:00Z", "2025-12-31 19:00:00 (-5)"},
		{"America/New_York", "2026-07-01T00:00:00Z", "2026-06-30 20:00:00 (-4)"},
		{"UTC", "2026-01-01T00:00:00Z", "2026-01-01 00:00:00 (+0)"},
	} {
		loc, err := time.LoadLocation(tc.zone)
		if err != nil {
			t.Fatal(err)
		}
		instant, err := time.Parse(time.RFC3339, tc.utc)
		if err != nil {
			t.Fatal(err)
		}
		if got := formatTimestamp(instant.UnixMilli(), loc); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.zone, got, tc.want)
		}
	}
	if got := formatTimestamp(0, time.UTC); got != "—" {
		t.Fatal("missing timestamp rendered as a date", got)
	}
}

func TestDisplayTimezoneDefaultAndInvalidConfiguration(t *testing.T) {
	s, err := storage.Open(filepath.Join(t.TempDir(), "table.db"), 20, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, _, err := s.CreateUser("Alice"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISPLAY_TIMEZONE", "")
	var out bytes.Buffer
	if err := printManagement(&out, s, false); err != nil || !strings.Contains(out.String(), "(+8)") {
		t.Fatal("default timezone not applied", err, out.String())
	}
	t.Setenv("DISPLAY_TIMEZONE", "Invalid/Timezone")
	out.Reset()
	if err := printManagement(&out, s, false); err == nil || out.Len() != 0 {
		t.Fatal("invalid timezone silently accepted")
	}
}
