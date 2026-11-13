package storage

import (
	"database/sql"
	"time"
)

type ManagedUser struct {
	Name                    string
	Devices, Online         int
	RecentContent, CreatedAt int64
}

type ManagedDevice struct {
	User, Name, Platform, Version, IP string
	Online                          bool
	LastActive, CreatedAt            int64
}

func (s *Store) RecordActivity(id, platform, version, ip string) error {
	_, err := s.db.Exec(`UPDATE devices SET last_active=?, recent_ip=?,
	platform=CASE WHEN ?='' THEN platform ELSE ? END,
	client_version=CASE WHEN ?='' THEN client_version ELSE ? END WHERE id=?`,
		time.Now().UnixMilli(), ip, platform, platform, version, version, id)
	return err
}

func (s *Store) TouchDevice(id string) error {
	_, err := s.db.Exec("UPDATE devices SET last_active=? WHERE id=?", time.Now().UnixMilli(), id)
	return err
}

// A short-lived snapshot lets the separate CLI process read server presence.
func (s *Store) SavePresence(ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM presence"); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err = tx.Exec("INSERT OR IGNORE INTO presence SELECT id FROM devices WHERE id=?", id); err != nil {
			return err
		}
	}
	_, err = tx.Exec("INSERT OR REPLACE INTO server_status VALUES(1,?,?)", time.Now().UnixMilli(), s.Retention.Milliseconds())
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ClearPresence() error {
	_, err := s.db.Exec("UPDATE server_status SET updated_at=0 WHERE id=1")
	return err
}

func (s *Store) ManagementLists() ([]ManagedUser, []ManagedDevice, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, false, err
	}
	defer tx.Rollback()
	var updated, retention int64
	err = tx.QueryRow("SELECT updated_at,retention_ms FROM server_status WHERE id=1").Scan(&updated, &retention)
	if err != nil && err != sql.ErrNoRows {
		return nil, nil, false, err
	}
	if retention == 0 {
		retention = s.Retention.Milliseconds()
	}
	age := time.Now().UnixMilli() - updated
	known := updated > 0 && age >= 0 && age <= 10000
	rows, err := tx.Query(`SELECT u.name,
	(SELECT count(*) FROM devices d WHERE d.user_id=u.id),
	(SELECT count(*) FROM devices d JOIN presence p ON p.device_id=d.id WHERE d.user_id=u.id),
	COALESCE((SELECT max(created_at) FROM entries e WHERE e.user_id=u.id AND e.created_at>=?),0),u.created_at
	FROM users u ORDER BY u.name COLLATE NOCASE,u.name`, time.Now().UnixMilli()-retention)
	if err != nil {
		return nil, nil, false, err
	}
	users := []ManagedUser{}
	for rows.Next() {
		var u ManagedUser
		if err = rows.Scan(&u.Name, &u.Devices, &u.Online, &u.RecentContent, &u.CreatedAt); err != nil {
			rows.Close()
			return nil, nil, false, err
		}
		users = append(users, u)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, false, err
	}
	rows, err = tx.Query(`SELECT u.name,d.name,d.platform,d.client_version,d.recent_ip,
	EXISTS(SELECT 1 FROM presence p WHERE p.device_id=d.id),d.last_active,d.created_at
	FROM devices d JOIN users u ON u.id=d.user_id ORDER BY u.name COLLATE NOCASE,u.name,d.name COLLATE NOCASE,d.name`)
	if err != nil {
		return nil, nil, false, err
	}
	devices := []ManagedDevice{}
	for rows.Next() {
		var d ManagedDevice
		if err = rows.Scan(&d.User, &d.Name, &d.Platform, &d.Version, &d.IP, &d.Online, &d.LastActive, &d.CreatedAt); err != nil {
			rows.Close()
			return nil, nil, false, err
		}
		devices = append(devices, d)
	}
	err = rows.Err()
	rows.Close()
	return users, devices, known, err
}
