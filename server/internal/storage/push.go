package storage

import "database/sql"

type PushTarget struct {
	DeviceID string
	Token    string
}

// A token belongs to one paired device, even after reinstalling or switching accounts.
func (s *Store) SetPushToken(device Device, token string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	if err = tx.QueryRow("SELECT id FROM devices WHERE id=? AND user_id=?", device.ID, device.UserID).Scan(&id); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM push_tokens WHERE device_id=? OR token=?", id, token); err != nil {
		return err
	}
	if token != "" {
		if _, err = tx.Exec("INSERT INTO push_tokens(device_id,token) VALUES(?,?)", id, token); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) PushTargets(userID, skipDeviceID string) ([]PushTarget, error) {
	rows, err := s.db.Query("SELECT p.device_id,p.token FROM push_tokens p JOIN devices d ON d.id=p.device_id WHERE d.user_id=? AND d.id<>? ORDER BY d.id", userID, skipDeviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var targets []PushTarget
	for rows.Next() {
		var target PushTarget
		if err = rows.Scan(&target.DeviceID, &target.Token); err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

// Recheck queued work against current ownership and entry lifetime before sending.
func (s *Store) PushPending(target PushTarget, userID, entryID string) (bool, error) {
	if _, err := s.Entry(userID, entryID); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	var exists bool
	err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM push_tokens p JOIN devices d ON d.id=p.device_id WHERE p.device_id=? AND p.token=? AND d.user_id=?)", target.DeviceID, target.Token, userID).Scan(&exists)
	return exists, err
}

func (s *Store) RemovePushToken(target PushTarget) error {
	_, err := s.db.Exec("DELETE FROM push_tokens WHERE device_id=? AND token=?", target.DeviceID, target.Token)
	return err
}
