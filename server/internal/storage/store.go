package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Device struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`
	Name   string `json:"name"`
}
type Entry struct {
	ID             string      `json:"id"`
	UserID         string      `json:"userId"`
	SenderID       string      `json:"senderId"`
	SourceDeviceID string      `json:"sourceDeviceId"`
	Kind           string      `json:"kind"`
	Text           string      `json:"text"`
	CreatedAt      int64       `json:"createdAt"`
	Attachment     *Attachment `json:"attachment,omitempty"`
}
type Attachment struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	MIME   string `json:"mime"`
	Size   int64  `json:"size"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}
type Store struct {
	db           *sql.DB
	files        string
	fileMu       sync.Mutex
	HistoryLimit int
	Retention    time.Duration
}

func Open(path string, limit int, retention time.Duration) (*Store, error) {
	if limit < 1 || retention <= 0 {
		return nil, fmt.Errorf("history limit and retention must be positive")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
	CREATE TABLE IF NOT EXISTS users(id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, invite_hash TEXT NOT NULL UNIQUE);
	CREATE TABLE IF NOT EXISTS devices(id TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE);
	CREATE TABLE IF NOT EXISTS entries(id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, sender_id INTEGER NOT NULL, source_device_id TEXT NOT NULL, kind TEXT NOT NULL, text TEXT NOT NULL, created_at INTEGER NOT NULL);
	CREATE INDEX IF NOT EXISTS entries_user_id ON entries(user_id,id DESC);
	CREATE TABLE IF NOT EXISTS blobs(id TEXT PRIMARY KEY, name TEXT NOT NULL, mime TEXT NOT NULL, size INTEGER NOT NULL, width INTEGER NOT NULL, height INTEGER NOT NULL);
	CREATE TABLE IF NOT EXISTS entry_files(entry_id INTEGER PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE, blob_id TEXT NOT NULL REFERENCES blobs(id));
	CREATE INDEX IF NOT EXISTS entry_files_blob ON entry_files(blob_id);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	files := path + ".attachments"
	if err = os.MkdirAll(files, 0700); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, files: files, HistoryLimit: limit, Retention: retention}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func secret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (s *Store) CreateUser(name string) (User, string, error) {
	invite := secret()
	result, err := s.db.Exec("INSERT INTO users(name,invite_hash) VALUES(?,?)", name, hash(invite))
	if err != nil {
		return User{}, "", err
	}
	id, err := result.LastInsertId()
	return User{fmt.Sprint(id), name}, invite, err
}
func (s *Store) Users() ([]User, error) {
	rows, err := s.db.Query("SELECT id,name FROM users ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name); err != nil {
			return nil, err
		}
		items = append(items, u)
	}
	return items, rows.Err()
}
func (s *Store) Connect(invite, name string) (Device, User, string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Device{}, User{}, "", err
	}
	defer tx.Rollback()
	var user User
	if err = tx.QueryRow("SELECT id,name FROM users WHERE invite_hash=?", hash(invite)).Scan(&user.ID, &user.Name); err != nil {
		return Device{}, User{}, "", err
	}
	token := secret()
	device := Device{secret(), user.ID, name}
	_, err = tx.Exec("INSERT INTO devices(id,user_id,name,token_hash) VALUES(?,?,?,?)", device.ID, user.ID, name, hash(token))
	if err != nil {
		return Device{}, User{}, "", err
	}
	err = tx.Commit()
	return device, user, token, err
}
func (s *Store) Authenticate(token string) (Device, error) {
	var d Device
	err := s.db.QueryRow("SELECT id,user_id,name FROM devices WHERE token_hash=?", hash(token)).Scan(&d.ID, &d.UserID, &d.Name)
	return d, err
}
func (s *Store) Devices() ([]Device, error) {
	rows, err := s.db.Query("SELECT id,user_id,name FROM devices ORDER BY user_id,name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Device{}
	for rows.Next() {
		var d Device
		if err = rows.Scan(&d.ID, &d.UserID, &d.Name); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}
func (s *Store) DeleteDevice(id string) error {
	r, e := s.db.Exec("DELETE FROM devices WHERE id=?", id)
	return affected(r, e)
}
func (s *Store) DeleteUser(id string) error {
	r, e := s.db.Exec("DELETE FROM users WHERE id=?", id)
	return affected(r, e)
}
func affected(r sql.Result, e error) error {
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e == nil && n == 0 {
		return sql.ErrNoRows
	}
	return e
}
func (s *Store) ResetInvite(id string) (string, error) {
	tx, e := s.db.Begin()
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	invite := secret()
	r, e := tx.Exec("UPDATE users SET invite_hash=? WHERE id=?", hash(invite), id)
	if e = affected(r, e); e != nil {
		return "", e
	}
	if _, e = tx.Exec("DELETE FROM devices WHERE user_id=?", id); e != nil {
		return "", e
	}
	return invite, tx.Commit()
}
func (s *Store) CreateEntry(device Device, recipient, text string) (Entry, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()
	e, err := s.createEntry(tx, device, recipient, text, nil)
	if err != nil {
		return Entry{}, err
	}
	return e, tx.Commit()
}

func (s *Store) createEntry(tx *sql.Tx, device Device, recipient, text string, attachment *Attachment) (Entry, error) {
	// Recheck inside the transaction so a revoked device cannot upload with stale middleware state.
	var uid string
	if err := tx.QueryRow("SELECT user_id FROM devices WHERE id=?", device.ID).Scan(&uid); err != nil {
		return Entry{}, err
	}
	if uid != device.UserID {
		return Entry{}, sql.ErrNoRows
	}
	var exists int
	if err := tx.QueryRow("SELECT 1 FROM users WHERE id=?", recipient).Scan(&exists); err != nil {
		return Entry{}, err
	}
	e := Entry{UserID: recipient, SenderID: device.UserID, SourceDeviceID: device.ID, Kind: "text", Text: text, CreatedAt: time.Now().UnixMilli()}
	if attachment != nil {
		e.Kind = "image"
		e.Attachment = attachment
	}
	r, err := tx.Exec("INSERT INTO entries(user_id,sender_id,source_device_id,kind,text,created_at) VALUES(?,?,?,?,?,?)", e.UserID, e.SenderID, e.SourceDeviceID, e.Kind, e.Text, e.CreatedAt)
	if err != nil {
		return Entry{}, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return Entry{}, err
	}
	e.ID = fmt.Sprint(id)
	if attachment != nil {
		if _, err = tx.Exec("INSERT INTO entry_files(entry_id,blob_id) VALUES(?,?)", e.ID, attachment.ID); err != nil {
			return Entry{}, err
		}
	}
	_, err = tx.Exec("DELETE FROM entries WHERE user_id=? AND id NOT IN (SELECT id FROM entries WHERE user_id=? ORDER BY id DESC LIMIT ?)", recipient, recipient, s.HistoryLimit)
	if err != nil {
		return Entry{}, err
	}
	return e, nil
}

const columns = "e.id,e.user_id,e.sender_id,e.source_device_id,e.kind,e.text,e.created_at,COALESCE(b.id,''),COALESCE(b.name,''),COALESCE(b.mime,''),COALESCE(b.size,0),COALESCE(b.width,0),COALESCE(b.height,0)"
const entryTables = "entries e LEFT JOIN entry_files f ON f.entry_id=e.id LEFT JOIN blobs b ON b.id=f.blob_id"

func scanEntry(row interface{ Scan(...any) error }) (Entry, error) {
	var e Entry
	var a Attachment
	err := row.Scan(&e.ID, &e.UserID, &e.SenderID, &e.SourceDeviceID, &e.Kind, &e.Text, &e.CreatedAt, &a.ID, &a.Name, &a.MIME, &a.Size, &a.Width, &a.Height)
	if a.ID != "" {
		e.Attachment = &a
	}
	return e, err
}
func (s *Store) Entry(user, id string) (Entry, error) {
	return scanEntry(s.db.QueryRow("SELECT "+columns+" FROM "+entryTables+" WHERE e.user_id=? AND e.id=? AND e.created_at>=?", user, id, time.Now().Add(-s.Retention).UnixMilli()))
}
func (s *Store) Latest(user string) (Entry, error) {
	return scanEntry(s.db.QueryRow("SELECT "+columns+" FROM "+entryTables+" WHERE e.user_id=? AND e.created_at>=? ORDER BY e.id DESC LIMIT 1", user, time.Now().Add(-s.Retention).UnixMilli()))
}
func (s *Store) History(user string) ([]Entry, error) {
	rows, err := s.db.Query("SELECT "+columns+" FROM "+entryTables+" WHERE e.user_id=? AND e.created_at>=? ORDER BY e.id DESC LIMIT ?", user, time.Now().Add(-s.Retention).UnixMilli(), s.HistoryLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []Entry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
func (s *Store) Cleanup() error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM entries WHERE created_at<?", time.Now().Add(-s.Retention).UnixMilli()); err != nil {
		return err
	}
	rows, err := tx.Query("SELECT id FROM blobs WHERE NOT EXISTS (SELECT 1 FROM entry_files WHERE blob_id=blobs.id)")
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM blobs WHERE NOT EXISTS (SELECT 1 FROM entry_files WHERE blob_id=blobs.id)"); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	for _, id := range ids {
		_ = os.Remove(filepath.Join(s.files, id))
	}
	// Recover files orphaned by a crash or an interrupted download holding a file open.
	files, err := os.ReadDir(s.files)
	if err != nil {
		return err
	}
	for _, file := range files {
		info, e := file.Info()
		if e != nil || file.IsDir() || time.Since(info.ModTime()) < time.Hour {
			continue
		}
		var count int
		if err = s.db.QueryRow("SELECT COUNT(*) FROM blobs WHERE id=?", file.Name()).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if err = os.Remove(filepath.Join(s.files, file.Name())); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func (s *Store) CreateImage(device Device, data []byte, attachment Attachment) (Entry, error) {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	attachment.ID = secret()
	attachment.Size = int64(len(data))
	path := filepath.Join(s.files, attachment.ID)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return Entry{}, err
	}
	success := false
	defer func() {
		if !success {
			_ = os.Remove(path)
		}
	}()
	tx, err := s.db.Begin()
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()
	_, err = tx.Exec("INSERT INTO blobs(id,name,mime,size,width,height) VALUES(?,?,?,?,?,?)", attachment.ID, attachment.Name, attachment.MIME, attachment.Size, attachment.Width, attachment.Height)
	if err != nil {
		return Entry{}, err
	}
	e, err := s.createEntry(tx, device, device.UserID, "", &attachment)
	if err != nil {
		return Entry{}, err
	}
	if err = tx.Commit(); err != nil {
		return Entry{}, err
	}
	success = true
	return e, nil
}

func (s *Store) Forward(device Device, id, recipient string) (Entry, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()
	original, err := scanEntry(tx.QueryRow("SELECT "+columns+" FROM "+entryTables+" WHERE e.user_id=? AND e.id=? AND e.created_at>=?", device.UserID, id, time.Now().Add(-s.Retention).UnixMilli()))
	if err != nil {
		return Entry{}, err
	}
	e, err := s.createEntry(tx, device, recipient, original.Text, original.Attachment)
	if err != nil {
		return Entry{}, err
	}
	return e, tx.Commit()
}

func (s *Store) OpenImage(user, id string) (*os.File, *Attachment, error) {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	e, err := s.Entry(user, id)
	if err != nil {
		return nil, nil, err
	}
	if e.Attachment == nil {
		return nil, nil, sql.ErrNoRows
	}
	f, err := os.Open(filepath.Join(s.files, e.Attachment.ID))
	return f, e.Attachment, err
}
