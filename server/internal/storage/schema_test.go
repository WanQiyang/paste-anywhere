package storage

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRejectUnsupportedDatabases(t *testing.T) {
	for _, setup := range []string{
		"CREATE TABLE users(id INTEGER PRIMARY KEY,name TEXT)",
		"PRAGMA user_version=2",
	} {
		t.Run(setup, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "unsupported.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(setup); err != nil {
				db.Close()
				t.Fatal(err)
			}
			db.Close()
			if s, err := Open(path, 20, time.Hour); err == nil {
				s.Close()
				t.Fatal("unsupported schema was accepted")
			} else if !strings.Contains(err.Error(), "不支持此数据库格式") {
				t.Fatal(err)
			}
		})
	}
}
