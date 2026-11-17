package storage

import (
	"database/sql"
	"fmt"
)

const schemaVersion = 3

// Only empty databases and the current schema are supported.
func initializeSchema(db *sql.DB) error {
	if _, err := db.Exec("PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;"); err != nil {
		return err
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != schemaVersion {
		var tables int
		if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
			return err
		}
		if version != 0 || tables != 0 {
			return fmt.Errorf("不支持此数据库格式，请清空数据目录后重新创建")
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		_, err = tx.Exec(`
		CREATE TABLE users(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE CHECK(length(name) BETWEEN 1 AND 128 AND name NOT GLOB '*[^A-Za-z0-9]*'),
			invite_hash TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL);
		CREATE TABLE devices(
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 128 AND name NOT GLOB '*[^A-Za-z0-9]*'),
			token_hash TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL,
			platform TEXT NOT NULL DEFAULT '',
			client_version TEXT NOT NULL DEFAULT '',
			recent_ip TEXT NOT NULL DEFAULT '',
			last_active INTEGER NOT NULL DEFAULT 0,
			UNIQUE(user_id,name));
		CREATE TABLE entries(id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, sender_id INTEGER NOT NULL, source_device_id TEXT NOT NULL, kind TEXT NOT NULL, text TEXT NOT NULL, created_at INTEGER NOT NULL);
		CREATE INDEX entries_user_id ON entries(user_id,id DESC);
		CREATE TABLE blobs(id TEXT PRIMARY KEY, name TEXT NOT NULL, mime TEXT NOT NULL, size INTEGER NOT NULL, width INTEGER NOT NULL, height INTEGER NOT NULL);
		CREATE TABLE entry_files(entry_id INTEGER PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE, blob_id TEXT NOT NULL REFERENCES blobs(id));
		CREATE INDEX entry_files_blob ON entry_files(blob_id);
		CREATE TABLE presence(device_id TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE);
		CREATE TABLE push_tokens(device_id TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE, token TEXT NOT NULL UNIQUE);
		CREATE TABLE server_status(id INTEGER PRIMARY KEY CHECK(id=1), updated_at INTEGER NOT NULL, retention_ms INTEGER NOT NULL);
		PRAGMA user_version=3;`)
		if err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	_, err := db.Exec("PRAGMA journal_mode=WAL;")
	return err
}
