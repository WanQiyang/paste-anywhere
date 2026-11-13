package storage

import (
	"crypto/rand"
	"fmt"
)

// ResetPairingCode atomically replaces the code and revokes every paired device.
func (s *Store) ResetPairingCode(name string) (string, error) {
	if !ValidName(name) {
		return "", ErrInvalidName
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	for attempt := 0; attempt < 8; attempt++ {
		invite := rand.Text()[:8]
		digest := hash(invite)
		// Acquire the write lock before reading, and reject both collisions and
		// the existing code. Pairing cannot interleave with device revocation.
		result, err := tx.Exec("UPDATE OR IGNORE users SET invite_hash=? WHERE name=? AND invite_hash<>?", digest, name, digest)
		if err != nil {
			return "", err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return "", err
		}
		var id int64
		if err := tx.QueryRow("SELECT id FROM users WHERE name=?", name).Scan(&id); err != nil {
			return "", err
		}
		if count == 0 {
			continue
		}
		if _, err := tx.Exec("DELETE FROM devices WHERE user_id=?", id); err != nil {
			return "", err
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return invite, nil
	}
	return "", fmt.Errorf("无法生成唯一配对码，请重试")
}
